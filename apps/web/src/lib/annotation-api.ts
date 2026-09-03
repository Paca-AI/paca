import { queryOptions } from "@tanstack/react-query";
import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// Mirrors environment-api.ts's own conventions exactly — types, REST
// functions, and queryOptions(...) factories. Backs both the extension's
// own API surface (apps/extension, called directly against this same
// backend, not through this file) and this web app's port-forward Comments
// tab + comment detail page — see
// services/api/internal/transport/http/dto/annotation_dto.go for the
// canonical shape these types mirror. A comment belongs to one specific
// port forward's running app, not the environment as a whole (an
// environment can have several port forwards) — every function/query here
// is keyed by (projectId, environmentId, portForwardId, ...) accordingly.

export interface BoundingBox {
	x_pct: number;
	y_pct: number;
	width_pct: number;
	height_pct: number;
	viewport_width: number;
	viewport_height: number;
}

export interface ElementSnapshot {
	tag_name: string;
	text_excerpt: string;
	outer_html_excerpt: string;
	accessible_name: string;
	role: string;
}

export interface ConsoleEntry {
	level: string;
	message: string;
	timestamp: string;
}

export interface FailedRequest {
	method: string;
	url: string;
	status_code: number;
	error?: string;
}

// created_by_name/created_by_username/created_by_avatar_(thumb_)url are
// denormalized author display fields the backend already resolves at read
// time (see annotationdom.Author) — avatar URLs are presigned and absent
// when the user has no avatar set, never a raw key to resolve client-side.
export interface AnnotationComment {
	id: string;
	body: string;
	created_by: string;
	created_by_name: string;
	created_by_username: string;
	created_by_avatar_url?: string;
	created_by_avatar_thumb_url?: string;
	created_at: string;
	updated_at: string;
}

export interface PageAnnotation {
	id: string;
	project_id: string;
	environment_id: string;
	port_forward_id: string;
	page_path: string;
	element_selector: string;
	element_selector_fallbacks: string[];
	bounding_box: BoundingBox;
	element_snapshot: ElementSnapshot;
	console_errors: ConsoleEntry[];
	failed_requests: FailedRequest[];
	screenshot_file_id: string | null;
	body: string;
	status: "open" | "resolved";
	task_id: string | null;
	created_by: string;
	created_by_name: string;
	created_by_username: string;
	created_by_avatar_url?: string;
	created_by_avatar_thumb_url?: string;
	resolved_by: string | null;
	resolved_at: string | null;
	created_at: string;
	updated_at: string;
	comments: AnnotationComment[];
}

function annotationsBasePath(
	projectId: string,
	environmentId: string,
	portForwardId: string,
): string {
	return `/projects/${projectId}/environments/${environmentId}/port-forwards/${portForwardId}/annotations`;
}

export async function listAnnotations(
	projectId: string,
	environmentId: string,
	portForwardId: string,
): Promise<PageAnnotation[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ annotations: PageAnnotation[] }>
	>(annotationsBasePath(projectId, environmentId, portForwardId));
	return data.data.annotations;
}

export async function getAnnotation(
	projectId: string,
	environmentId: string,
	portForwardId: string,
	annotationId: string,
): Promise<PageAnnotation> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<PageAnnotation>
	>(
		`${annotationsBasePath(projectId, environmentId, portForwardId)}/${annotationId}`,
	);
	return data.data;
}

// Project-scoped single fetch, unlike getAnnotation above which additionally
// requires (and the backend ignores) environment/port-forward IDs from the
// nested route. For callers that only have a project ID + annotation ID —
// e.g. resolving a comment attached to a conversation via the
// `?annotationId=` query param on the new-conversation page.
export async function getAnnotationInProject(
	projectId: string,
	annotationId: string,
): Promise<PageAnnotation> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<PageAnnotation>
	>(`/projects/${projectId}/annotations/${annotationId}`);
	return data.data;
}

export async function resolveAnnotation(
	projectId: string,
	environmentId: string,
	portForwardId: string,
	annotationId: string,
): Promise<PageAnnotation> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<PageAnnotation>
	>(
		`${annotationsBasePath(projectId, environmentId, portForwardId)}/${annotationId}/resolve`,
	);
	return data.data;
}

export async function reopenAnnotation(
	projectId: string,
	environmentId: string,
	portForwardId: string,
	annotationId: string,
): Promise<PageAnnotation> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<PageAnnotation>
	>(
		`${annotationsBasePath(projectId, environmentId, portForwardId)}/${annotationId}/reopen`,
	);
	return data.data;
}

export async function addComment(
	projectId: string,
	environmentId: string,
	portForwardId: string,
	annotationId: string,
	body: string,
): Promise<AnnotationComment> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<AnnotationComment>
	>(
		`${annotationsBasePath(projectId, environmentId, portForwardId)}/${annotationId}/comments`,
		{ body },
	);
	return data.data;
}

export async function createTaskFromAnnotation(
	projectId: string,
	environmentId: string,
	portForwardId: string,
	annotationId: string,
): Promise<PageAnnotation> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<PageAnnotation>
	>(
		`${annotationsBasePath(projectId, environmentId, portForwardId)}/${annotationId}/create-task`,
		{},
	);
	return data.data;
}

export async function getAnnotationScreenshotUrl(
	projectId: string,
	environmentId: string,
	portForwardId: string,
	annotationId: string,
): Promise<string> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ url: string }>
	>(
		`${annotationsBasePath(projectId, environmentId, portForwardId)}/${annotationId}/screenshot-url`,
	);
	return data.data.url;
}

export const annotationScreenshotUrlQueryOptions = (
	projectId: string,
	environmentId: string,
	portForwardId: string,
	annotation: PageAnnotation,
) =>
	queryOptions({
		queryKey: [
			"projects",
			projectId,
			"environments",
			environmentId,
			"port-forwards",
			portForwardId,
			"annotations",
			annotation.id,
			"screenshot-url",
		],
		queryFn: () =>
			getAnnotationScreenshotUrl(
				projectId,
				environmentId,
				portForwardId,
				annotation.id,
			),
		enabled: Boolean(annotation.screenshot_file_id),
		staleTime: 10 * 60 * 1000,
	});

export const portForwardAnnotationsQueryOptions = (
	projectId: string,
	environmentId: string,
	portForwardId: string,
) =>
	queryOptions({
		queryKey: [
			"projects",
			projectId,
			"environments",
			environmentId,
			"port-forwards",
			portForwardId,
			"annotations",
		],
		queryFn: () => listAnnotations(projectId, environmentId, portForwardId),
		enabled:
			Boolean(projectId) && Boolean(environmentId) && Boolean(portForwardId),
	});

export const annotationQueryOptions = (
	projectId: string,
	environmentId: string,
	portForwardId: string,
	annotationId: string,
) =>
	queryOptions({
		queryKey: [
			"projects",
			projectId,
			"environments",
			environmentId,
			"port-forwards",
			portForwardId,
			"annotations",
			annotationId,
		],
		queryFn: () =>
			getAnnotation(projectId, environmentId, portForwardId, annotationId),
	});
