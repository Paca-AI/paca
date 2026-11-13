import { Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { InlineNotice } from "@/components/shared/inline-notice";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import type { Sprint } from "@/lib/interaction-api";
import { cn } from "@/lib/utils";

export interface SprintFormPayload {
	name: string;
	goal: string | null;
	start_date: string | null;
	end_date: string | null;
	status?: "active";
}

interface SprintFormModalProps {
	/** "start" activates the sprint on submit (status: "active") and shows
	 * the already-active warning; "edit" saves field changes only and
	 * offers a delete action instead. */
	mode: "start" | "edit";
	sprint: Sprint;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onSubmit: (sprintId: string, payload: SprintFormPayload) => Promise<void>;
	/** start mode only: another sprint already active in the project, if
	 * any — shown as a non-blocking warning; Scrum favors one active
	 * sprint at a time, but starting a second one anyway is allowed. */
	otherActiveSprint?: Sprint | null;
	/** Externally-controlled error text (e.g. from the caller's mutation),
	 * shown below the fields. In edit mode this slot is also reused for
	 * delete failures, since deleting is triggered from this same dialog. */
	errorMessage?: string | null;
	/** edit mode only: renders a delete button in the footer's left slot. */
	onDelete?: () => void;
	/** edit mode only: true while a delete-confirmation dialog is stacked on
	 * top of this one, so this modal's own Escape handling doesn't also
	 * fire — Escape should close only the topmost dialog. */
	suppressEscape?: boolean;
}

export function SprintFormModal({
	mode,
	sprint,
	open,
	onOpenChange,
	onSubmit,
	otherActiveSprint,
	errorMessage,
	onDelete,
}: SprintFormModalProps) {
	const { t } = useTranslation("projects");
	const idPrefix = mode === "start" ? "ss" : "es";
	const [name, setName] = useState(sprint.name);
	const [goal, setGoal] = useState(sprint.goal ?? "");
	const [startDate, setStartDate] = useState(
		sprint.start_date
			? sprint.start_date.slice(0, 10)
			: mode === "start"
				? new Date().toISOString().slice(0, 10)
				: "",
	);
	const [endDate, setEndDate] = useState(
		sprint.end_date ? sprint.end_date.slice(0, 10) : "",
	);
	const [submitting, setSubmitting] = useState(false);

	// Reset form to the current sprint values when the modal closes
	useEffect(() => {
		if (!open) {
			const today = new Date().toISOString().slice(0, 10);
			setName(sprint.name);
			setGoal(sprint.goal ?? "");
			setStartDate(
				sprint.start_date
					? sprint.start_date.slice(0, 10)
					: mode === "start"
						? today
						: "",
			);
			setEndDate(sprint.end_date ? sprint.end_date.slice(0, 10) : "");
		}
	}, [sprint, open, mode]);

	const dateRangeInvalid = Boolean(startDate && endDate && endDate < startDate);

	const handleSubmit = async () => {
		setSubmitting(true);
		try {
			await onSubmit(sprint.id, {
				name: name.trim() || sprint.name,
				goal: goal.trim() || null,
				start_date: startDate ? `${startDate}T00:00:00Z` : null,
				end_date: endDate ? `${endDate}T00:00:00Z` : null,
				...(mode === "start" ? { status: "active" as const } : {}),
			});
			onOpenChange(false);
		} catch {
			// Swallowed — the caller surfaces failures via `errorMessage`.
		} finally {
			setSubmitting(false);
		}
	};

	const title =
		mode === "start"
			? t("layout.startSprintModal.title")
			: t("layout.sprintDetail.editSprintModal.title");
	const nameLabel =
		mode === "start"
			? t("layout.startSprintModal.nameLabel")
			: t("layout.sprintDetail.editSprintModal.nameLabel");
	const goalLabel =
		mode === "start"
			? t("layout.startSprintModal.goalLabel")
			: t("layout.sprintDetail.editSprintModal.goalLabel");
	const optionalLabel =
		mode === "start"
			? t("layout.startSprintModal.optional")
			: t("layout.sprintDetail.editSprintModal.optional");
	const goalPlaceholder =
		mode === "start"
			? t("layout.startSprintModal.goalPlaceholder")
			: t("layout.sprintDetail.editSprintModal.goalPlaceholder");
	const startDateLabel =
		mode === "start"
			? t("layout.startSprintModal.startDateLabel")
			: t("layout.sprintDetail.editSprintModal.startDateLabel");
	const endDateLabel =
		mode === "start"
			? t("layout.startSprintModal.endDateLabel")
			: t("layout.sprintDetail.editSprintModal.dueDateLabel");
	const cancelLabel =
		mode === "start"
			? t("layout.startSprintModal.cancel")
			: t("layout.sprintDetail.editSprintModal.cancel");
	const submitLabel =
		mode === "start"
			? t("layout.startSprintModal.startSprint")
			: t("layout.sprintDetail.editSprintModal.save");
	const submittingLabel =
		mode === "start"
			? t("layout.startSprintModal.starting")
			: t("layout.sprintDetail.editSprintModal.saving");

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="max-w-md sm:max-w-md">
				<DialogHeader>
					<DialogTitle className="font-[Syne] text-lg font-bold tracking-tight">
						{title}
					</DialogTitle>
				</DialogHeader>
				{otherActiveSprint && (
					<Alert className="border-warning/30 bg-warning/10 text-warning">
						<AlertDescription className="text-warning">
							{t("layout.startSprintModal.alreadyActiveWarning", {
								name: otherActiveSprint.name,
							})}
						</AlertDescription>
					</Alert>
				)}
				<div className="flex flex-col gap-4">
					<div className="flex flex-col gap-1.5">
						<Label htmlFor={`${idPrefix}-name`}>{nameLabel}</Label>
						<Input
							id={`${idPrefix}-name`}
							value={name}
							onChange={(e) => setName(e.target.value)}
						/>
					</div>
					<div className="flex flex-col gap-1.5">
						<Label
							htmlFor={`${idPrefix}-goal`}
							className="text-muted-foreground"
						>
							{goalLabel}{" "}
							<span className="text-xs font-normal">{optionalLabel}</span>
						</Label>
						<Textarea
							id={`${idPrefix}-goal`}
							value={goal}
							onChange={(e) => setGoal(e.target.value)}
							rows={4}
							placeholder={goalPlaceholder}
							className="resize-y"
						/>
					</div>
					<div className="grid grid-cols-2 gap-3">
						<div className="flex flex-col gap-1.5">
							<Label
								htmlFor={`${idPrefix}-start`}
								className="text-muted-foreground"
							>
								{startDateLabel}
							</Label>
							<Input
								id={`${idPrefix}-start`}
								type="date"
								value={startDate}
								onChange={(e) => setStartDate(e.target.value)}
							/>
						</div>
						<div className="flex flex-col gap-1.5">
							<Label
								htmlFor={`${idPrefix}-end`}
								className="text-muted-foreground"
							>
								{endDateLabel}
							</Label>
							<Input
								id={`${idPrefix}-end`}
								type="date"
								value={endDate}
								min={startDate || undefined}
								onChange={(e) => setEndDate(e.target.value)}
							/>
						</div>
					</div>
					{dateRangeInvalid && (
						<p className="text-xs text-destructive">
							{t("layout.sprintDetail.editSprintModal.dateRangeError")}
						</p>
					)}
					{errorMessage && (
						<InlineNotice tone="error">{errorMessage}</InlineNotice>
					)}
				</div>
				<DialogFooter className="sm:justify-between">
					{onDelete ? (
						<Button type="button" variant="destructive" onClick={onDelete}>
							<Trash2 data-icon="inline-start" />
							{t("layout.sprintDetail.editSprintModal.delete")}
						</Button>
					) : (
						<div />
					)}
					<div className="flex gap-2">
						<Button
							type="button"
							variant="outline"
							onClick={() => onOpenChange(false)}
						>
							{cancelLabel}
						</Button>
						<Button
							type="button"
							onClick={handleSubmit}
							disabled={
								submitting ||
								(mode === "edit" && !name.trim()) ||
								dateRangeInvalid
							}
							className={cn(
								mode === "start" && "bg-success text-white hover:bg-success/90",
							)}
						>
							{submitting ? submittingLabel : submitLabel}
						</Button>
					</div>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
