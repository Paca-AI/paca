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
// are non-chat or extremely niche). Anything unmapped here falls back to
// initials exactly as before.
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
};

// "custom" has no fixed provider identity, so it's intentionally unmapped —
// falls back to initials like any other unmapped provider. "goose" is
// likewise unmapped for now, not because it lacks an identity but because no
// Goose asset has been sourced from @lobehub/icons-static-svg yet (see the
// module doc comment) — add an entry once one has, rather than guessing at
// a path that doesn't exist on disk.
const ACP_PROVIDER_LOGOS: Record<string, string> = {
	"claude-code": "/provider-logos/claude-code.svg",
	codex: "/provider-logos/codex.svg",
	"gemini-cli": "/provider-logos/gemini-cli.svg",
};

function lookupProviderLogo(
	agentType: string | undefined,
	llmProvider: string | undefined,
	acpProvider: string | null | undefined,
): string | undefined {
	if (agentType === "acp") {
		return acpProvider ? ACP_PROVIDER_LOGOS[acpProvider] : undefined;
	}
	return llmProvider
		? LLM_PROVIDER_LOGOS[llmProvider.toLowerCase()]
		: undefined;
}

/**
 * Returns the default avatar for an agent's configured provider, or
 * undefined if the provider isn't mapped (caller should fall back to
 * initials in that case, same as when the agent has no avatar at all).
 */
function getDefaultAgentAvatar(
	agent: Pick<Agent, "agent_type" | "llm_provider" | "acp_provider">,
): string | undefined {
	return lookupProviderLogo(
		agent.agent_type,
		agent.llm_provider,
		agent.acp_provider,
	);
}

/**
 * getDefaultAgentAvatar's sibling for the lighter-weight ProjectMember
 * projection (team page, assignee/reporter pickers, task chips) — same
 * lookup, different field names since those come from a JOIN rather than
 * the full Agent record. Returns undefined for human members.
 */
function getDefaultMemberAvatar(
	member: Pick<
		ProjectMember,
		"member_type" | "agent_type" | "agent_llm_provider" | "agent_acp_provider"
	>,
): string | undefined {
	if (member.member_type !== "agent") return undefined;
	return lookupProviderLogo(
		member.agent_type,
		member.agent_llm_provider,
		member.agent_acp_provider,
	);
}

// ── Resolved avatar URL — the single "avatar -> default avatar" step ───────────
//
// Every display site ultimately wants one thing: the URL to render, with a
// real upload always winning over the provider-logo default. Previously each
// call site re-derived that itself via `x.avatar_thumb_url ?? getDefaultXAvatar(x)`
// — easy to typo the field, forget the `??`, or omit the default entirely (all
// three happened across different components over time). These two functions
// are the only place that priority is expressed; every caller goes through
// one of them instead of inlining the merge. The remaining, final tier —
// falling back to initials/an icon when this returns undefined — stays with
// the rendering components (EntityAvatarContent / AvatarUpload), since that
// part is display markup, not a resolution rule.

/**
 * Resolves an agent's own record to the avatar URL to render: its real
 * upload if present, else its provider-logo default, else undefined. `size`
 * picks which uploaded variant to prefer — "full" for large headers, "thumb"
 * (default) for every small chip/list use.
 */
export function resolveAgentAvatarUrl(
	agent: Pick<
		Agent,
		| "avatar_url"
		| "avatar_thumb_url"
		| "agent_type"
		| "llm_provider"
		| "acp_provider"
	>,
	size: "full" | "thumb" = "thumb",
): string | undefined {
	const uploaded = size === "full" ? agent.avatar_url : agent.avatar_thumb_url;
	return uploaded ?? getDefaultAgentAvatar(agent);
}

/**
 * resolveAgentAvatarUrl's sibling for the lighter-weight ProjectMember
 * projection (team page, assignee/reporter pickers, task chips). Always
 * thumb-sized — ProjectMember-based surfaces never need the full variant.
 */
export function resolveMemberAvatarUrl(
	member: Pick<
		ProjectMember,
		| "avatar_thumb_url"
		| "member_type"
		| "agent_type"
		| "agent_llm_provider"
		| "agent_acp_provider"
	>,
): string | undefined {
	return member.avatar_thumb_url ?? getDefaultMemberAvatar(member);
}

/**
 * getDefaultMemberAvatar's sibling for the notification list's actor
 * projection (notification-bell.tsx) — same lookup, different field names
 * (an `actor_` prefix) since these come from the notification JOIN rather
 * than the project_members JOIN. Returns undefined for human actors.
 */
function getDefaultNotificationActorAvatar(
	n: Pick<
		Notification,
		| "actor_member_type"
		| "actor_agent_type"
		| "actor_agent_llm_provider"
		| "actor_agent_acp_provider"
	>,
): string | undefined {
	if (n.actor_member_type !== "agent") return undefined;
	return lookupProviderLogo(
		n.actor_agent_type,
		n.actor_agent_llm_provider,
		n.actor_agent_acp_provider,
	);
}

/**
 * resolveMemberAvatarUrl's sibling for the notification list's actor
 * projection (notification-bell.tsx). Always thumb-sized, same as
 * resolveMemberAvatarUrl.
 */
export function resolveNotificationActorAvatarUrl(
	n: Pick<
		Notification,
		| "actor_avatar_thumb_url"
		| "actor_member_type"
		| "actor_agent_type"
		| "actor_agent_llm_provider"
		| "actor_agent_acp_provider"
	>,
): string | undefined {
	return n.actor_avatar_thumb_url ?? getDefaultNotificationActorAvatar(n);
}
