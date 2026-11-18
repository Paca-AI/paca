import type { Agent } from "./agent-api";
import type { Notification } from "./notification-api";
import type { ProjectMember } from "./project-api";

// Default agent avatar placeholders, shown when an agent has no custom
// uploaded avatar — one recognizable brand mark per LLM/ACP provider instead
// of bare initials. Assets are static SVGs served from public/provider-logos
// (sourced from @lobehub/icons-static-svg, MIT licensed), not part of the
// upload/S3 pipeline: they're a pure display fallback, same tier as the
// initials fallback they replace.
//
// Deliberately covers only the mainstream providers users are realistically
// likely to pick — not the full ~115-entry raw LLM catalog (most of which
// are non-chat or extremely niche). Anything unmapped here falls back to the
// Paca avatar (see resolveAvatarSrc below).
const LLM_PROVIDER_LOGOS: Record<string, string> = {
	anthropic: "/provider-logos/anthropic.svg",
	openai: "/provider-logos/openai.svg",
	gemini: "/provider-logos/gemini.svg",
	mistral: "/provider-logos/mistral.svg",
	cohere: "/provider-logos/cohere.svg",
	xai: "/provider-logos/xai.svg",
	deepseek: "/provider-logos/deepseek.svg",
	groq: "/provider-logos/groq.svg",
	perplexity: "/provider-logos/perplexity.svg",
	openrouter: "/provider-logos/openrouter.svg",
	ollama: "/provider-logos/ollama.svg",
	azure: "/provider-logos/azure.svg",
	bedrock: "/provider-logos/bedrock.svg",
	meta_llama: "/provider-logos/meta_llama.svg",
	vertex_ai: "/provider-logos/vertex_ai.svg",
	fluxionai: "/provider-logos/fluxionai.svg",
};

// "custom" has no fixed provider identity, so it's intentionally unmapped —
// falls back to the Paca avatar like any other unmapped provider. "goose" is
// likewise unmapped for now, not because it lacks an identity but because no
// Goose asset has been sourced from @lobehub/icons-static-svg yet (see the
// module doc comment) — add an entry once one has, rather than guessing at
// a path that doesn't exist on disk.
const ACP_PROVIDER_LOGOS: Record<string, string> = {
	"claude-code": "/provider-logos/claude-code.svg",
	codex: "/provider-logos/codex.svg",
	"gemini-cli": "/provider-logos/gemini-cli.svg",
	opencode: "/provider-logos/opencode.svg",
};

function lookupProviderLogo(
	agentType: string | undefined,
	llmProvider: string | undefined,
	acpProvider: string | null | undefined,
	cliProvider?: string | null,
): string | undefined {
	if (agentType === "acp") {
		return acpProvider ? ACP_PROVIDER_LOGOS[acpProvider] : undefined;
	}
	if (agentType === "provider_cli") {
		// Same brand marks as ACP_PROVIDER_LOGOS — cli_provider and
		// acp_provider name the same underlying CLI, just via two different
		// execution paths (see agentdom.Agent.CLIProvider's doc comment).
		// cursor-agent has no entry (no asset sourced yet — same reasoning
		// as "goose" above) and falls back to the Paca avatar.
		return cliProvider ? ACP_PROVIDER_LOGOS[cliProvider] : undefined;
	}
	return llmProvider
		? LLM_PROVIDER_LOGOS[llmProvider.toLowerCase()]
		: undefined;
}

// ── Avatar resolution — the single priority chain ──────────────────────────────
//
// Every avatar in the app is rendered by <EntityAvatar>, which asks this module
// what to show. The chain lives only here:
//
//   human            uploaded avatar  -> initials
//   agent            uploaded avatar  -> provider logo -> Paca avatar
//   service account  uploaded avatar  -> Paca avatar
//
// The adapters below (agentAvatar / memberAvatar / notificationActorAvatar /
// ...) map each API shape onto <EntityAvatar>'s props, so a call site is
// `<EntityAvatar {...memberAvatar(m)} />` and never re-derives the chain.

/** What kind of account an avatar belongs to; decides the fallback chain. */
export type AvatarSubject = "human" | "agent" | "service_account";

/** The provider fields that pick an agent's default (provider-logo) avatar. */
export interface AgentProviderInfo {
	agentType?: string | null;
	llmProvider?: string | null;
	acpProvider?: string | null;
	cliProvider?: string | null;
}

/** The Paca mark, the last-resort avatar for agents and service accounts. */
export const PACA_AVATAR_LIGHT = "/paca-logo.svg";
export const PACA_AVATAR_DARK = "/paca-logo-dark.svg";

/**
 * Resolves the image to show for an avatar, or undefined when the chain ends
 * in initials (humans only) or in the Paca mark (see EntityAvatar, which
 * draws the mark itself so it can follow the light/dark theme).
 */
export function resolveAvatarSrc(
	type: AvatarSubject,
	avatarUrl: string | null | undefined,
	agent?: AgentProviderInfo,
): string | undefined {
	if (avatarUrl) return stableSignedUrl(avatarUrl);
	if (type !== "agent" || !agent) return undefined;
	return lookupProviderLogo(
		agent.agentType ?? undefined,
		agent.llmProvider ?? undefined,
		agent.acpProvider,
		agent.cliProvider,
	);
}

// The API signs a stored avatar's URL afresh on every response, so the same
// avatar arrives with a different query string each time a list is fetched
// (every page of tasks, every refetch). Handing each one to <img> makes the
// browser download every avatar again, and they blink back to their initials
// meanwhile. The first URL seen for an avatar is kept and reused until it is
// old, well before its one-hour signature expires.
// Shared with IdDirectory; both derive from the API's 1h avatar URL TTL.
// Only S3-style presigned URLs (X-Amz-*) are recognised.
export const SIGNED_URL_KEEP_MS = 20 * 60 * 1000;
const MAX_SIGNED_URLS = 500;
// Deliberately module state, written while rendering: every component showing
// the same avatar must agree on one URL, so it cannot live in a component.
const signedUrls = new Map<
	string,
	{ url: string; at: number; signedAt: number }
>();

export function isSignedUrl(url: string): boolean {
	return url.includes("X-Amz-Signature=");
}

/** When the URL was signed (from X-Amz-Date), or `fallback` if unreadable. */
function signedAtOf(url: string, fallback: number): number {
	const m = /[?&]X-Amz-Date=(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})Z/.exec(
		url,
	);
	if (!m) return fallback;
	const t = Date.UTC(+m[1], +m[2] - 1, +m[3], +m[4], +m[5], +m[6]);
	return Number.isNaN(t) ? fallback : t;
}

export function stableSignedUrl(url: string, now = Date.now()): string {
	if (!isSignedUrl(url)) return url;
	const base = url.split("?")[0];
	const kept = signedUrls.get(base);
	if (kept && now - kept.at < SIGNED_URL_KEEP_MS) return kept.url;
	const signedAt = signedAtOf(url, now);
	// A stale copy (say from cached query data) must not replace a fresher
	// kept URL, or it could be served past its signature's expiry.
	if (kept && signedAt < kept.signedAt) return kept.url;
	if (signedUrls.size >= MAX_SIGNED_URLS) {
		for (const [key, v] of signedUrls) {
			if (now - v.at >= SIGNED_URL_KEEP_MS) signedUrls.delete(key);
		}
		if (signedUrls.size >= MAX_SIGNED_URLS) signedUrls.clear();
	}
	signedUrls.set(base, { url, at: now, signedAt });
	return url;
}

/** Props <EntityAvatar> takes from an API record (see the adapters below). */
export interface AvatarIdentity {
	name: string;
	type: AvatarSubject;
	/** The account's own uploaded avatar; never a provider-logo default. */
	avatarUrl?: string | null;
	agent?: AgentProviderInfo;
}

function agentProviderInfo(
	agent: Pick<
		Agent,
		"agent_type" | "llm_provider" | "acp_provider" | "cli_provider"
	>,
): AgentProviderInfo {
	return {
		agentType: agent.agent_type,
		llmProvider: agent.llm_provider,
		acpProvider: agent.acp_provider,
		cliProvider: agent.cli_provider,
	};
}

/**
 * An Agent record. `size` picks which uploaded variant to prefer: "full" for
 * large headers, "thumb" (default) for every small chip/list use.
 */
export function agentAvatar(
	agent: Pick<
		Agent,
		| "name"
		| "avatar_url"
		| "avatar_thumb_url"
		| "agent_type"
		| "llm_provider"
		| "acp_provider"
		| "cli_provider"
	>,
	size: "full" | "thumb" = "thumb",
): AvatarIdentity {
	return {
		name: agent.name,
		type: "agent",
		avatarUrl: size === "full" ? agent.avatar_url : agent.avatar_thumb_url,
		agent: agentProviderInfo(agent),
	};
}

/** A service account record. */
export function serviceAccountAvatar(account: {
	name: string;
	avatar_url?: string | null;
	avatar_thumb_url?: string | null;
}): AvatarIdentity {
	return {
		name: account.name,
		type: "service_account",
		avatarUrl: account.avatar_thumb_url,
	};
}

/** A user record (profile, admin user list, user menu). */
export function userAvatar(user: {
	full_name?: string;
	username?: string;
	avatar_url?: string | null;
	avatar_thumb_url?: string | null;
}): AvatarIdentity {
	return {
		name: user.full_name || user.username || "",
		type: "human",
		avatarUrl: user.avatar_thumb_url,
	};
}

/** A project (sidebar switcher, project cards, settings): initials fallback. */
export function projectAvatar(project: {
	name: string;
	avatar_thumb_url?: string | null;
}): AvatarIdentity {
	return {
		name: project.name,
		type: "human",
		avatarUrl: project.avatar_thumb_url,
	};
}

/** Maps an API `member_type` string onto an AvatarSubject. */
export function avatarSubjectOf(memberType: string | undefined): AvatarSubject {
	if (memberType === "agent") return "agent";
	if (memberType === "service_account") return "service_account";
	return "human";
}

/** The lighter-weight ProjectMember projection (team page, pickers, chips). */
export function memberAvatar(
	member: Pick<
		ProjectMember,
		| "full_name"
		| "username"
		| "avatar_thumb_url"
		| "member_type"
		| "agent_type"
		| "agent_llm_provider"
		| "agent_acp_provider"
		| "agent_cli_provider"
	>,
): AvatarIdentity {
	return {
		name: member.full_name || member.username,
		type: avatarSubjectOf(member.member_type),
		avatarUrl: member.avatar_thumb_url,
		agent: {
			agentType: member.agent_type,
			llmProvider: member.agent_llm_provider,
			acpProvider: member.agent_acp_provider,
			cliProvider: member.agent_cli_provider,
		},
	};
}

/** The notification list's actor projection (notification-bell.tsx). */
export function notificationActorAvatar(
	n: Pick<
		Notification,
		| "actor_full_name"
		| "actor_username"
		| "actor_avatar_thumb_url"
		| "actor_member_type"
		| "actor_agent_type"
		| "actor_agent_llm_provider"
		| "actor_agent_acp_provider"
		| "actor_agent_cli_provider"
	>,
): AvatarIdentity {
	return {
		name: n.actor_full_name || n.actor_username,
		type: avatarSubjectOf(n.actor_member_type),
		avatarUrl: n.actor_avatar_thumb_url,
		agent: {
			agentType: n.actor_agent_type,
			llmProvider: n.actor_agent_llm_provider,
			acpProvider: n.actor_agent_acp_provider,
			cliProvider: n.actor_agent_cli_provider,
		},
	};
}
