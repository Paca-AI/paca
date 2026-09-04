import { Trash2, X } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import type { Sprint } from "@/lib/interaction-api";

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
	suppressEscape,
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

	// Register a document-level keydown listener while the modal is open so
	// Escape works regardless of which element currently has focus
	useEffect(() => {
		if (!open || suppressEscape) return;
		const handleKeyDown = (e: KeyboardEvent) => {
			if (e.key === "Escape") onOpenChange(false);
		};
		document.addEventListener("keydown", handleKeyDown);
		return () => document.removeEventListener("keydown", handleKeyDown);
	}, [open, suppressEscape, onOpenChange]);

	if (!open) return null;

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
		// biome-ignore lint/a11y/noStaticElementInteractions: modal backdrop
		<div
			className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm"
			onClick={(e) => {
				if (e.target === e.currentTarget) onOpenChange(false);
			}}
			onKeyDown={(e) => {
				if (e.key === "Escape") onOpenChange(false);
			}}
		>
			{/* biome-ignore lint/a11y/noStaticElementInteractions: modal panel */}
			<div
				className="relative w-full max-w-md rounded-xl border border-border/50 bg-background p-6 shadow-2xl mx-4"
				onClick={(e) => e.stopPropagation()}
				onKeyDown={(e) => e.stopPropagation()}
			>
				<button
					type="button"
					onClick={() => onOpenChange(false)}
					className="absolute right-4 top-4 flex size-7 items-center justify-center rounded-md text-muted-foreground/60 hover:text-foreground hover:bg-muted/60 transition-all"
				>
					<X className="size-4" />
				</button>
				<h2 className="font-[Syne] text-lg font-bold tracking-tight mb-4">
					{title}
				</h2>
				{otherActiveSprint && (
					<p className="mb-4 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-400">
						{t("layout.startSprintModal.alreadyActiveWarning", {
							name: otherActiveSprint.name,
						})}
					</p>
				)}
				<div className="flex flex-col gap-4">
					<div className="flex flex-col gap-1.5">
						<label htmlFor={`${idPrefix}-name`} className="text-sm font-medium">
							{nameLabel}
						</label>
						<input
							id={`${idPrefix}-name`}
							value={name}
							onChange={(e) => setName(e.target.value)}
							className="rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-primary/30 placeholder:text-muted-foreground/50"
						/>
					</div>
					<div className="flex flex-col gap-1.5">
						<label
							htmlFor={`${idPrefix}-goal`}
							className="text-sm font-medium text-muted-foreground"
						>
							{goalLabel}{" "}
							<span className="text-xs font-normal">{optionalLabel}</span>
						</label>
						<textarea
							id={`${idPrefix}-goal`}
							value={goal}
							onChange={(e) => setGoal(e.target.value)}
							rows={4}
							placeholder={goalPlaceholder}
							className="rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-primary/30 placeholder:text-muted-foreground/50 resize-y"
						/>
					</div>
					<div className="grid grid-cols-2 gap-3">
						<div className="flex flex-col gap-1.5">
							<label
								htmlFor={`${idPrefix}-start`}
								className="text-sm font-medium text-muted-foreground"
							>
								{startDateLabel}
							</label>
							<input
								id={`${idPrefix}-start`}
								type="date"
								value={startDate}
								onChange={(e) => setStartDate(e.target.value)}
								className="rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-primary/30"
							/>
						</div>
						<div className="flex flex-col gap-1.5">
							<label
								htmlFor={`${idPrefix}-end`}
								className="text-sm font-medium text-muted-foreground"
							>
								{endDateLabel}
							</label>
							<input
								id={`${idPrefix}-end`}
								type="date"
								value={endDate}
								min={startDate || undefined}
								onChange={(e) => setEndDate(e.target.value)}
								className="rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-primary/30"
							/>
						</div>
					</div>
					{dateRangeInvalid && (
						<p className="text-xs text-destructive">
							{t("layout.sprintDetail.editSprintModal.dateRangeError")}
						</p>
					)}
					{errorMessage && (
						<p className="text-xs text-destructive bg-destructive/10 rounded-lg px-3 py-2">
							{errorMessage}
						</p>
					)}
				</div>
				<div className="mt-6 flex items-center justify-between gap-2">
					{onDelete ? (
						<button
							type="button"
							onClick={onDelete}
							className="flex items-center gap-1.5 rounded-lg border border-destructive/40 bg-destructive/10 px-4 py-2 text-sm font-semibold text-destructive hover:bg-destructive/20 transition-all"
						>
							<Trash2 className="size-3.5 shrink-0" />
							{t("layout.sprintDetail.editSprintModal.delete")}
						</button>
					) : (
						<div />
					)}
					<div className="flex gap-2">
						<button
							type="button"
							onClick={() => onOpenChange(false)}
							className="rounded-lg border border-border/50 bg-muted/20 px-4 py-2 text-sm font-medium hover:bg-muted/40 transition-all"
						>
							{cancelLabel}
						</button>
						<button
							type="button"
							onClick={handleSubmit}
							disabled={
								submitting ||
								(mode === "edit" && !name.trim()) ||
								dateRangeInvalid
							}
							className={
								mode === "start"
									? "rounded-lg bg-emerald-600 px-4 py-2 text-sm font-semibold text-white hover:bg-emerald-700 disabled:opacity-50 transition-all"
									: "rounded-lg bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground hover:bg-primary/90 disabled:opacity-50 transition-all"
							}
						>
							{submitting ? submittingLabel : submitLabel}
						</button>
					</div>
				</div>
			</div>
		</div>
	);
}
