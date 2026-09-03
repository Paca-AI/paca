import type {
	CreateAnnotationRequest,
	PageAnnotation,
	PortForwardMatch,
} from "../shared/types";

// Every call here runs from the content script itself, directly against
// the Paca API — credentials: "include" works because this content script
// only ever runs on the same hostname the Paca app itself is served from
// (different port, same site — see services/api's corsMiddleware and the
// extension's README), so access_token/refresh_token are attached by the
// browser exactly as they are for the web app's own requests. No token of
// any kind is read or stored by this extension.

export class ApiError extends Error {
	constructor(
		public status: number,
		message: string,
	) {
		super(message);
	}
}

// services/api wraps every success response as {"data": ...} (see
// presenter.OK) — every call here needs to unwrap that envelope, not
// return it as-is.
interface Envelope<T> {
	data: T;
}

function rawFetch(
	baseUrl: string,
	path: string,
	init?: RequestInit,
): Promise<Response> {
	return fetch(`${baseUrl}/api/v1${path}`, {
		...init,
		credentials: "include",
		headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
	});
}

// Coalesces concurrent refresh attempts into one in-flight request — a
// page with several pins can easily fire a handful of calls at once right
// as the 15-minute access token expires, and they'd otherwise all race
// each other into /auth/refresh. Mirrors apps/web's own axios interceptor,
// just without a request queue: refreshAccessToken's callers each retry
// their own single request once it resolves.
let refreshInFlight: Promise<boolean> | null = null;

function refreshAccessToken(baseUrl: string): Promise<boolean> {
	if (!refreshInFlight) {
		refreshInFlight = rawFetch(baseUrl, "/auth/refresh", { method: "POST" })
			.then((res) => res.ok)
			.catch(() => false)
			.finally(() => {
				refreshInFlight = null;
			});
	}
	return refreshInFlight;
}

async function toResult<T>(
	res: Response,
	method: string,
	path: string,
): Promise<T> {
	if (!res.ok) {
		throw new ApiError(res.status, `${method} ${path} failed: ${res.status}`);
	}
	if (res.status === 204) return undefined as T;
	const body = (await res.json()) as Envelope<T>;
	return body.data;
}

// A 401 gets exactly one refresh-and-retry, mirroring apps/web's own
// interceptor — if the refreshed request still 401s (or the refresh call
// itself fails, e.g. the refresh token has also expired), that failure
// propagates normally rather than looping.
async function request<T>(
	baseUrl: string,
	path: string,
	init?: RequestInit,
): Promise<T> {
	const method = init?.method ?? "GET";
	let res = await rawFetch(baseUrl, path, init);
	if (res.status === 401 && (await refreshAccessToken(baseUrl))) {
		res = await rawFetch(baseUrl, path, init);
	}
	return toResult<T>(res, method, path);
}

export function resolvePortForward(
	baseUrl: string,
	hostPort: number,
): Promise<PortForwardMatch> {
	return request(baseUrl, `/port-forwards/resolve?host_port=${hostPort}`);
}

// A comment belongs to one specific port forward's running app, not the
// environment as a whole (an environment can have several port forwards) —
// every annotation endpoint is nested under it accordingly.
function annotationsBasePath(
	projectId: string,
	environmentId: string,
	portForwardId: string,
): string {
	return `/projects/${projectId}/environments/${environmentId}/port-forwards/${portForwardId}/annotations`;
}

export async function listAnnotationsForPage(
	baseUrl: string,
	projectId: string,
	environmentId: string,
	portForwardId: string,
	pagePath: string,
): Promise<PageAnnotation[]> {
	const resp = await request<{ annotations: PageAnnotation[] }>(
		baseUrl,
		`${annotationsBasePath(projectId, environmentId, portForwardId)}?page_path=${encodeURIComponent(pagePath)}`,
	);
	return resp.annotations;
}

export function createAnnotation(
	baseUrl: string,
	projectId: string,
	environmentId: string,
	portForwardId: string,
	body: CreateAnnotationRequest,
): Promise<PageAnnotation> {
	return request(
		baseUrl,
		annotationsBasePath(projectId, environmentId, portForwardId),
		{
			method: "POST",
			body: JSON.stringify(body),
		},
	);
}

export function resolveAnnotation(
	baseUrl: string,
	projectId: string,
	environmentId: string,
	portForwardId: string,
	annotationId: string,
): Promise<PageAnnotation> {
	return request(
		baseUrl,
		`${annotationsBasePath(projectId, environmentId, portForwardId)}/${annotationId}/resolve`,
		{ method: "PATCH" },
	);
}

export function reopenAnnotation(
	baseUrl: string,
	projectId: string,
	environmentId: string,
	portForwardId: string,
	annotationId: string,
): Promise<PageAnnotation> {
	return request(
		baseUrl,
		`${annotationsBasePath(projectId, environmentId, portForwardId)}/${annotationId}/reopen`,
		{ method: "PATCH" },
	);
}

export function createTaskFromAnnotation(
	baseUrl: string,
	projectId: string,
	environmentId: string,
	portForwardId: string,
	annotationId: string,
): Promise<PageAnnotation> {
	return request(
		baseUrl,
		`${annotationsBasePath(projectId, environmentId, portForwardId)}/${annotationId}/create-task`,
		{ method: "POST", body: JSON.stringify({}) },
	);
}

export function addComment(
	baseUrl: string,
	projectId: string,
	environmentId: string,
	portForwardId: string,
	annotationId: string,
	body: string,
): Promise<PageAnnotation> {
	return request(
		baseUrl,
		`${annotationsBasePath(projectId, environmentId, portForwardId)}/${annotationId}/comments`,
		{ method: "POST", body: JSON.stringify({ body }) },
	);
}

export async function uploadScreenshot(
	baseUrl: string,
	projectId: string,
	environmentId: string,
	portForwardId: string,
	pngBlob: Blob,
): Promise<string> {
	const session = await request<{ file_id: string; upload_url: string }>(
		baseUrl,
		`${annotationsBasePath(projectId, environmentId, portForwardId)}/upload-url`,
		{
			method: "POST",
			body: JSON.stringify({
				file_name: "screenshot.png",
				content_type: "image/png",
				file_size: pngBlob.size,
			}),
		},
	);
	// The presigned URL carries its own auth in the query string — a plain
	// cross-origin PUT with no credentials, the same pattern the main web
	// app already uses for direct-to-object-store uploads. Not routed
	// through request()/rawFetch(): it's not a services/api call at all,
	// so neither the /api/v1 prefix nor the refresh-on-401 handling apply.
	const putRes = await fetch(session.upload_url, {
		method: "PUT",
		headers: { "Content-Type": "image/png" },
		body: pngBlob,
	});
	if (!putRes.ok) {
		throw new ApiError(
			putRes.status,
			`screenshot upload failed: ${putRes.status}`,
		);
	}
	return session.file_id;
}
