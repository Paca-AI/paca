import { X } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import type { Sprint } from "@/lib/interaction-api";

interface StartSprintModalProps {
	sprint: Sprint;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onSubmit: (
		sprintId: string,
		payload: {
			name: string;
			goal: string | null;
			start_date: string | null;
			end_date: string | null;
			status: "active";
		},
	) => Promise<void>;
	/** Another sprint already active in the project, if any — shown as a
	 * non-blocking warning; Scrum favors one active sprint at a time, but
	 * starting a second one anyway is allowed. */
	otherActiveSprint?: Sprint | null;
}

export function StartSprintModal({
	sprint,
	open,
	onOpenChange,
	onSubmit,
	otherActiveSprint,
}: StartSprintModalProps) {
	const { t } = useTranslation("projects");
	const [name, setName] = useState(sprint.name);
	const [goal, setGoal] = useState(sprint.goal ?? "");
	const [startDate, setStartDate] = useState(
		sprint.start_date
			? sprint.start_date.slice(0, 10)
			: new Date().toISOString().slice(0, 10),
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
			setStartDate(sprint.start_date ? sprint.start_date.slice(0, 10) : today);
			setEndDate(sprint.end_date ? sprint.end_date.slice(0, 10) : "");
		}
	}, [sprint, open]);

	// Register a document-level keydown listener while the modal is open so
	// Escape works regardless of which element currently has focus
	useEffect(() => {
		if (!open) return;
		const handleKeyDown = (e: KeyboardEvent) => {
			if (e.key === "Escape") onOpenChange(false);
		};
		document.addEventListener("keydown", handleKeyDown);
		return () => document.removeEventListener("keydown", handleKeyDown);
	}, [open, onOpenChange]);

	if (!open) return null;

	const handleSubmit = async () => {
		setSubmitting(true);
		try {
			await onSubmit(sprint.id, {
				name: name.trim() || sprint.name,
				goal: goal.trim() || null,
				start_date: startDate ? `${startDate}T00:00:00Z` : null,
				end_date: endDate ? `${endDate}T00:00:00Z` : null,
				status: "active",
			});
			onOpenChange(false);
		} finally {
			setSubmitting(false);
		}
	};

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
					{t("layout.startSprintModal.title")}
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
						<label htmlFor="ss-name" className="text-sm font-medium">
							{t("layout.startSprintModal.nameLabel")}
						</label>
						<input
							id="ss-name"
							value={name}
							onChange={(e) => setName(e.target.value)}
							className="rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-primary/30 placeholder:text-muted-foreground/50"
						/>
					</div>
					<div className="flex flex-col gap-1.5">
						<label
							htmlFor="ss-goal"
							className="text-sm font-medium text-muted-foreground"
						>
							{t("layout.startSprintModal.goalLabel")}{" "}
							<span className="text-xs font-normal">
								{t("layout.startSprintModal.optional")}
							</span>
						</label>
						<textarea
							id="ss-goal"
							value={goal}
							onChange={(e) => setGoal(e.target.value)}
							rows={2}
							placeholder={t("layout.startSprintModal.goalPlaceholder")}
							className="rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-primary/30 placeholder:text-muted-foreground/50 resize-none"
						/>
					</div>
					<div className="grid grid-cols-2 gap-3">
						<div className="flex flex-col gap-1.5">
							<label
								htmlFor="ss-start"
								className="text-sm font-medium text-muted-foreground"
							>
								{t("layout.startSprintModal.startDateLabel")}
							</label>
							<input
								id="ss-start"
								type="date"
								value={startDate}
								onChange={(e) => setStartDate(e.target.value)}
								className="rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-primary/30"
							/>
						</div>
						<div className="flex flex-col gap-1.5">
							<label
								htmlFor="ss-end"
								className="text-sm font-medium text-muted-foreground"
							>
								{t("layout.startSprintModal.endDateLabel")}
							</label>
							<input
								id="ss-end"
								type="date"
								value={endDate}
								min={startDate || undefined}
								onChange={(e) => setEndDate(e.target.value)}
								className="rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-primary/30"
							/>
						</div>
					</div>
				</div>
				<div className="mt-6 flex justify-end gap-2">
					<button
						type="button"
						onClick={() => onOpenChange(false)}
						className="rounded-lg border border-border/50 bg-muted/20 px-4 py-2 text-sm font-medium hover:bg-muted/40 transition-all"
					>
						{t("layout.startSprintModal.cancel")}
					</button>
					<button
						type="button"
						onClick={handleSubmit}
						disabled={submitting}
						className="rounded-lg bg-emerald-600 px-4 py-2 text-sm font-semibold text-white hover:bg-emerald-700 disabled:opacity-50 transition-all"
					>
						{submitting
							? t("layout.startSprintModal.starting")
							: t("layout.startSprintModal.startSprint")}
					</button>
				</div>
			</div>
		</div>
	);
}
