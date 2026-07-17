import type { PacaConfig } from "./types/index.js";

/**
 * Authentication modes for the Paca MCP server (Galaxy fork, ADR-038).
 *
 * - "apikey"  — upstream behavior: X-API-Key (+ optional X-Agent-ID header
 *               impersonation, which the Galaxy API rejects by design).
 * - "bearer"  — Galaxy platform mode: a single RS256 platform token in
 *               `Authorization: Bearer …`. The token carries the effective
 *               principal in its signed act_as claim; NO identity headers
 *               are ever sent (identity from signed tokens, never headers).
 */
export type PacaAuthMode = "apikey" | "bearer";

/**
 * Raised when the API rejects our platform bearer token (HTTP 401).
 * Once raised, every subsequent request in this process fails immediately
 * with the same error — a static token cannot become valid again, so
 * retrying only burns agent iterations (the MaxIterations loop bug).
 */
export class AuthTokenRejectedError extends Error {
	constructor(message: string, options?: ErrorOptions) {
		super(message, options);
		this.name = "AuthTokenRejectedError";
	}
}

const REJECTED_MESSAGE =
	"Paca API rejected the platform bearer token (HTTP 401): the token has " +
	"expired or its act_as principal is not a provisioned SSO user. This is " +
	"NOT transient — every Paca tool call in this session will fail the same " +
	"way, so do NOT retry. Report the failure and finish the task without " +
	"Paca write-backs (the conversation was likely triggered by a non-SSO " +
	"actor, or ran past the platform token TTL).";

/** Latched after the first bearer 401 — see AuthTokenRejectedError. */
let bearerRejected = false;

/** Test hook: clear the bearer-rejection latch. */
export function resetAuthRejection(): void {
	bearerRejected = false;
}

/**
 * Reads and validates the auth mode configuration from the environment.
 *
 * Throws on an unknown PACA_AUTH_MODE and — fail fast at startup — when
 * bearer mode is selected without a PACA_MCP_TOKEN.
 */
export function resolveAuthEnv(env: Record<string, string | undefined>): {
	authMode: PacaAuthMode;
	bearerToken?: string;
} {
	const raw = (env.PACA_AUTH_MODE || "apikey").trim().toLowerCase();
	if (raw !== "apikey" && raw !== "bearer") {
		throw new Error(
			`PACA_AUTH_MODE must be "apikey" or "bearer", got "${raw}".`,
		);
	}
	if (raw === "bearer") {
		const bearerToken = env.PACA_MCP_TOKEN?.trim();
		if (!bearerToken) {
			throw new Error(
				"PACA_AUTH_MODE=bearer requires PACA_MCP_TOKEN (the RS256 platform " +
					"token minted by the ai-agent service). Refusing to start without " +
					"it — a bearer-mode server with no token could only ever fail.",
			);
		}
		return { authMode: "bearer", bearerToken };
	}
	return { authMode: "apikey" };
}

/**
 * Builds the authentication headers for a Paca API request.
 *
 * bearer mode: `Authorization: Bearer <token>` only — never X-API-Key and
 * never X-Agent-ID (the principal lives in the token's signed act_as claim).
 *
 * apikey mode (default): upstream behavior, X-API-Key plus X-Agent-ID when
 * an agent id is configured.
 *
 * In bearer mode this throws immediately once a 401 has been observed
 * (see registerAuthResponse) so no further network calls are attempted.
 */
export function buildAuthHeaders(config: PacaConfig): Record<string, string> {
	if (config.authMode === "bearer") {
		if (bearerRejected) {
			throw new AuthTokenRejectedError(REJECTED_MESSAGE);
		}
		if (!config.bearerToken) {
			throw new AuthTokenRejectedError(
				"PACA_AUTH_MODE=bearer but no bearer token is configured — " +
					"cannot authenticate any Paca API request.",
			);
		}
		return { Authorization: `Bearer ${config.bearerToken}` };
	}

	const headers: Record<string, string> = { "X-API-Key": config.apiKey };
	if (config.agentId) {
		headers["X-Agent-ID"] = config.agentId;
	}
	return headers;
}

/**
 * Records an API response status for auth purposes. Call after every fetch.
 *
 * In bearer mode a 401 latches the process-wide rejection flag and throws
 * AuthTokenRejectedError with a clear, do-not-retry message. In apikey mode
 * this is a no-op (upstream error handling applies).
 */
export function registerAuthResponse(
	status: number,
	config: PacaConfig,
): void {
	if (config.authMode === "bearer" && status === 401) {
		bearerRejected = true;
		throw new AuthTokenRejectedError(REJECTED_MESSAGE);
	}
}
