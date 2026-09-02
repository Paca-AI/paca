import { queryOptions } from "@tanstack/react-query";
import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// Mirrors environment-api.ts's own conventions exactly — types, REST
// functions, and queryOptions(...) factories. Backs both the extension's
// own API surface (apps/extension, called directly against this same
// backend, not through this file) and this web app's "Comments" view — see
// services/api/internal/transport/http/dto/annotation_dto.go for the
// canonical shape these types mirror.

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

export interface AnnotationComment {
	id: string;
	body: string;
	created_by: string;
	created_at: string;
	updated_at: string;
}

export interface PageAnnotation {
	id: string;
	project_id: string;
	environment_id: string;
	port_forward_id: string | null;
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
	resolved_by: string | null;
	resolved_at: string | null;
	created_at: string;
	updated_at: string;
	comments: AnnotationComment[];
}

export async function listAnnotations(
	projectId: string,
	environmentId: string,
): Promise<PageAnnotation[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ annotations: PageAnnotation[] }>
	>(`/projects/${projectId}/environments/${environmentId}/annotations`);
	return data.data.annotations;
}

export async function resolveAnnotation(
	projectId: string,
	environmentId: string,
	annotationId: string,
): Promise<PageAnnotation> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<PageAnnotation>
	>(
		`/projects/${projectId}/environments/${environmentId}/annotations/${annotationId}/resolve`,
	);
	return data.data;
}

export async function reopenAnnotation(
	projectId: string,
	environmentId: string,
	annotationId: string,
): Promise<PageAnnotation> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<PageAnnotation>
	>(
		`/projects/${projectId}/environments/${environmentId}/annotations/${annotationId}/reopen`,
	);
	return data.data;
}

export async function createTaskFromAnnotation(
	projectId: string,
	environmentId: string,
	annotationId: string,
): Promise<PageAnnotation> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<PageAnnotation>
	>(
		`/projects/${projectId}/environments/${environmentId}/annotations/${annotationId}/create-task`,
		{},
	);
	return data.data;
}

export async function getAnnotationScreenshotUrl(
	projectId: string,
	environmentId: string,
	annotationId: string,
): Promise<string> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ url: string }>
	>(
		`/projects/${projectId}/environments/${environmentId}/annotations/${annotationId}/screenshot-url`,
	);
	return data.data.url;
}

export const annotationScreenshotUrlQueryOptions = (
	projectId: string,
	environmentId: string,
	annotation: PageAnnotation,
) =>
	queryOptions({
		queryKey: [
			"projects",
			projectId,
			"environments",
			environmentId,
			"annotations",
			annotation.id,
			"screenshot-url",
		],
		queryFn: () =>
			getAnnotationScreenshotUrl(projectId, environmentId, annotation.id),
		enabled: Boolean(annotation.screenshot_file_id),
		staleTime: 10 * 60 * 1000,
	});

export const environmentAnnotationsQueryOptions = (
	projectId: string,
	environmentId: string,
) =>
	queryOptions({
		queryKey: [
			"projects",
			projectId,
			"environments",
			environmentId,
			"annotations",
		],
		queryFn: () => listAnnotations(projectId, environmentId),
		enabled: Boolean(projectId) && Boolean(environmentId),
	});
