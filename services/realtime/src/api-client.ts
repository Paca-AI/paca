// API client used by the realtime service to delegate authentication and
// permission lookups to services/api.
//
// The realtime service never verifies JWTs locally — the API is the single
// source of truth. If the API rejects the token (expired, revoked, malformed)
// the socket connection is refused or the operation is rejected.

export interface AuthResult {
	userId: string;
	username: string;
	role: string;
	globalPermissions: string[];
	// expiresAt is the token's exp claim in epoch milliseconds, or undefined
	// when it carries none. A socket must not outlive it (see server.ts).
	expiresAt?: number;
}

// Thrown by the API calls below so callers can distinguish an expired/invalid
// token (401 — the caller may want to force a reconnect) from any other
// failure (network blip, API 5xx) without parsing the error message.
export class ApiRequestError extends Error {
	constructor(
		message: string,
		readonly status: number,
	) {
		super(message);
		this.name = "ApiRequestError";
	}
}

// All API responses are wrapped in a standard success envelope:
// { success: true, data: <T> }
interface ApiEnvelope<T> {
	success: boolean;
	data: T;
}

interface GlobalPermissionsData {
	permissions: string[];
}

interface ProjectPermissionsData {
	actions: string[];
}

// verifyTokenWithAPI calls GET /api/v1/users/me/global-permissions using the
// supplied access token. A successful 200 response proves the token is valid;
// the response body also provides the caller's global permission list.
// User identity fields (userId, username, role) are extracted from the JWT
// payload without signature verification — they are trusted because the API
// has already verified the signature.
export async function verifyTokenWithAPI(
	apiUrl: string,
	token: string,
): Promise<AuthResult> {
	const res = await fetch(`${apiUrl}/api/v1/users/me/global-permissions`, {
		headers: { Authorization: `Bearer ${token}` },
		signal: AbortSignal.timeout(8_000),
	});

	if (!res.ok) {
		throw new ApiRequestError(
			`API auth rejected token: HTTP ${res.status}`,
			res.status,
		);
	}

	const envelope = (await res.json()) as ApiEnvelope<GlobalPermissionsData>;

	// Decode identity claims from the JWT payload (no signature check — the
	// API call above is the verification).
	const { sub, username, role, exp } = decodePayload(token);

	return {
		userId: sub,
		username,
		role,
		expiresAt: exp === undefined ? undefined : exp * 1000,
		globalPermissions: Array.isArray(envelope.data?.permissions)
			? envelope.data.permissions
			: [],
	};
}

// fetchProjectPermissions calls GET /api/v1/projects/:projectId/members/me/permissions
// to retrieve the authenticated user's effective IAM actions in a project
// ("tasks:read", ...), returned as a map of action -> true. The list includes
// an action allowed only on some resource inside the project (one sprint's
// tasks): it decides which rooms a socket may be in, not what it receives —
// every event is checked on its own resource (see AccessDecider).
// Returns null when the user is not a member of that project (API returns 404).
export async function fetchProjectPermissions(
	apiUrl: string,
	token: string,
	projectId: string,
): Promise<Record<string, boolean> | null> {
	const res = await fetch(
		`${apiUrl}/api/v1/projects/${encodeURIComponent(projectId)}/members/me/permissions`,
		{
			headers: { Authorization: `Bearer ${token}` },
			signal: AbortSignal.timeout(8_000),
		},
	);

	if (res.status === 404) return null; // not a project member
	if (!res.ok) {
		throw new ApiRequestError(
			`API project permissions failed: HTTP ${res.status}`,
			res.status,
		);
	}

	const envelope = (await res.json()) as ApiEnvelope<ProjectPermissionsData>;
	return Object.fromEntries(
		(envelope.data?.actions ?? []).map((action) => [action, true]),
	);
}

// entityReadable asks the API whether the token's account can open one entity
// through the REST route that serves it, scoped to the project the client
// named. A 200 means the entity exists in that project and the account may read
// it; a 403 or 404 means it does not (or is in another project). Anything else
// throws, so callers fail closed. It exists because the IAM probe only matches
// the resource string it is given: it cannot tell that task T is not in project
// P, while the project-scoped lookup behind the REST route can.
export async function entityReadable(
	apiUrl: string,
	token: string,
	path: string,
): Promise<boolean> {
	const res = await fetch(`${apiUrl}/api/v1${path}`, {
		headers: { Authorization: `Bearer ${token}` },
		signal: AbortSignal.timeout(8_000),
	});
	if (res.status === 403 || res.status === 404) return false;
	if (!res.ok) {
		throw new ApiRequestError(
			`API entity lookup failed: HTTP ${res.status}`,
			res.status,
		);
	}
	return true;
}

// AccessQuery is one question for checkAccess: may the token's account
// perform `action` on `resource` (an IAM resource name inside the project, or,
// with an empty projectId, one under a platform root such as "channel/<id>")?
export interface AccessQuery {
	action: string;
	resource: string;
}

// checkAccess asks the API, in one request, whether the token's account is
// allowed each query — POST /projects/:projectId/members/me/permissions/check,
// evaluated by the IAM engine that gates the REST routes, so entity-level Deny
// statements and conditions apply. The answers are in query order. Anything
// but a complete answer throws (a 401 as an ApiRequestError so the caller can
// force a fresh handshake): callers fail closed instead of caching a guess.
export async function checkAccess(
	apiUrl: string,
	token: string,
	projectId: string,
	queries: AccessQuery[],
): Promise<boolean[]> {
	// An empty project id asks about resources outside any project (a direct
	// message), which the API answers at /users/me/permissions/check.
	const path = projectId
		? `/projects/${encodeURIComponent(projectId)}/members/me/permissions/check`
		: "/users/me/permissions/check";
	const res = await fetch(`${apiUrl}/api/v1${path}`, {
		method: "POST",
		headers: {
			Authorization: `Bearer ${token}`,
			"Content-Type": "application/json",
		},
		body: JSON.stringify({ checks: queries }),
		signal: AbortSignal.timeout(8_000),
	});
	if (!res.ok) {
		throw new ApiRequestError(
			`API access check failed: HTTP ${res.status}`,
			res.status,
		);
	}
	const envelope = (await res.json()) as ApiEnvelope<{ allowed?: unknown }>;
	const allowed = envelope.data?.allowed;
	if (
		!Array.isArray(allowed) ||
		allowed.length !== queries.length ||
		!allowed.every((a) => typeof a === "boolean")
	) {
		throw new ApiRequestError("API access check: malformed answer", 502);
	}
	return allowed as boolean[];
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

interface JwtPayload {
	sub: string;
	username: string;
	role: string;
	exp?: number;
}

function decodePayload(token: string): JwtPayload {
	const parts = token.split(".");
	if (parts.length !== 3) throw new Error("malformed JWT: expected 3 parts");

	// Standard base64url decode; Buffer is available in Bun.
	if (!parts[1]) throw new Error("malformed JWT: empty payload");
	const raw = parts[1];
	const base64 = raw.replace(/-/g, "+").replace(/_/g, "/");
	const padded = base64 + "=".repeat((4 - (base64.length % 4)) % 4);
	const json = Buffer.from(padded, "base64").toString("utf8");

	const payload = JSON.parse(json) as Record<string, unknown>;

	const sub = payload.sub;
	const username = payload.username;
	const role = payload.role;

	if (typeof sub !== "string" || !sub)
		throw new Error("JWT payload missing sub");
	if (typeof username !== "string" || !username)
		throw new Error("JWT payload missing username");
	if (typeof role !== "string" || !role)
		throw new Error("JWT payload missing role");

	const exp = typeof payload.exp === "number" ? payload.exp : undefined;

	return { sub, username, role, exp };
}
