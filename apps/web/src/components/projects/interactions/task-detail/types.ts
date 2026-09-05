import type { Activity, Task } from "@/lib/interaction-api";
import type {
	CustomFieldOption,
	ProjectMember,
	TaskStatus,
	TaskType,
} from "@/lib/project-api";

// ── Extended types (UI-first, wired to API later) ─────────────────────────────

export interface CustomFieldDef {
	id: string;
	display_name: string;
	field_key: string;
	field_type:
		| "Text"
		| "Number"
		| "Date"
		| "Checkbox"
		| "Select"
		| "MultiSelect"
		| "Url";
	required?: boolean;
	options?: CustomFieldOption[];
}

export interface Attachment {
	id: string;
	name: string;
	size?: number;
	uploaded_at: string;
	url?: string;
}

// ActivityEntry mirrors the backend Activity type with a convenience re-export.
export type ActivityEntry = Activity;

// ── Component props ────────────────────────────────────────────────────────────

export interface TaskDetailModalProps {
	task: Task | null;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	statuses: TaskStatus[];
	taskTypes: TaskType[];
	members?: ProjectMember[];
	customFields?: CustomFieldDef[];
	projectName?: string;
	interactionName?: string;
	projectId?: string;
	taskIdPrefix?: string;
	mode?: "modal" | "page";
	canEdit?: boolean;
}
