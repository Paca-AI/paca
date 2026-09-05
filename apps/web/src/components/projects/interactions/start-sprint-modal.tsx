import type { Sprint } from "@/lib/interaction-api";
import { SprintFormModal, type SprintFormPayload } from "./sprint-form-modal";

interface StartSprintModalProps {
	sprint: Sprint;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onSubmit: (sprintId: string, payload: SprintFormPayload) => Promise<void>;
	/** Another sprint already active in the project, if any — shown as a
	 * non-blocking warning; Scrum favors one active sprint at a time, but
	 * starting a second one anyway is allowed. */
	otherActiveSprint?: Sprint | null;
	/** Externally-controlled error text (e.g. from the caller's mutation),
	 * shown below the fields when starting the sprint fails. */
	errorMessage?: string | null;
}

export function StartSprintModal({
	sprint,
	open,
	onOpenChange,
	onSubmit,
	otherActiveSprint,
	errorMessage,
}: StartSprintModalProps) {
	return (
		<SprintFormModal
			mode="start"
			sprint={sprint}
			open={open}
			onOpenChange={onOpenChange}
			onSubmit={onSubmit}
			otherActiveSprint={otherActiveSprint}
			errorMessage={errorMessage}
		/>
	);
}
