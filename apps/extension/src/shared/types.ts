// Shared types mirroring services/api's annotation DTOs
// (services/api/internal/transport/http/dto/annotation_dto.go) and the
// port-forward resolution response — kept in sync by hand since this
// extension is a separate package with no shared-types build step.

export interface PortForwardMatch {
	project_id: string;
	environment_id: string;
	port_forward_id: string;
	label: string;
}

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

// Author display fields are denormalized onto every annotation/comment by
// services/api at read time (see annotationdom.Author) — avatar URLs are
// presigned and may be absent (no avatar set), never a raw key to resolve
// client-side.
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

// port_forward_id isn't a field here — it's the owning resource in the URL
// (see content/api.ts's createAnnotation), not part of the request body.
export interface CreateAnnotationRequest {
	page_path: string;
	element_selector: string;
	element_selector_fallbacks: string[];
	bounding_box: BoundingBox;
	element_snapshot: ElementSnapshot;
	console_errors: ConsoleEntry[];
	failed_requests: FailedRequest[];
	screenshot_file_id: string | null;
	body: string;
}
