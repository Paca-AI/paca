import { Plus } from "lucide-react";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import type { TaskType } from "@/lib/project-api";
import { TaskTypeSelector } from "./task-type-selector";

interface AddTaskRowProps {
	taskTypes: TaskType[];
	onAdd: (title: string, taskTypeId: string | null) => void;
	/** "list" renders an inline row; "board" renders a card-style box */
	variant?: "list" | "board";
	/** Text for the closed-state trigger button */
	label?: string;
	/** Placeholder for the title input */
	placeholder?: string;
}

export function AddTaskRow({
	taskTypes,
	onAdd,
	variant = "list",
	label,
	placeholder,
}: AddTaskRowProps) {
	const { t } = useTranslation("projects");
	const resolvedLabel = label ?? t("board.addTaskRow.addTask");
	const resolvedPlaceholder = placeholder ?? t("board.addTaskRow.placeholder");
	const [open, setOpen] = useState(false);
	const [value, setValue] = useState("");
	const [selectedTypeId, setSelectedTypeId] = useState<string | null>(null);
	const inputRef = useRef<HTMLInputElement>(null);

	const defaultType =
		taskTypes.find((tt) => tt.is_default) ?? taskTypes[0] ?? null;
	const selectedType =
		taskTypes.find((tt) => tt.id === selectedTypeId) ?? defaultType;

	const openForm = () => {
		setOpen(true);
		setTimeout(() => inputRef.current?.focus(), 0);
	};

	const submit = () => {
		const title = value.trim();
		if (!title) return;
		onAdd(title, selectedType?.id ?? null);
		setValue("");
		setSelectedTypeId(null);
		setOpen(false);
	};

	const cancel = () => {
		setValue("");
		setSelectedTypeId(null);
		setOpen(false);
	};

	// Shared type-selector dropdown
	const typeSelector = taskTypes.length > 0 && selectedType && (
		<TaskTypeSelector
			taskTypes={taskTypes}
			value={selectedType.id}
			onChange={setSelectedTypeId}
		/>
	);

	// Shared action buttons
	const actionButtons = (
		<>
			<button
				type="button"
				onClick={cancel}
				className="flex items-center gap-1.5 rounded-lg bg-muted/40 text-muted-foreground/80 hover:bg-muted/60 hover:text-foreground px-2.5 py-1.5 text-xs font-semibold transition-all duration-150"
			>
				{t("board.addTaskRow.cancel")}
			</button>
			<button
				type="button"
				onClick={submit}
				disabled={!value.trim()}
				className="rounded-lg bg-primary px-3 py-1.5 text-xs font-semibold text-primary-foreground hover:bg-primary/90 shadow-sm disabled:opacity-40 transition-all duration-150"
			>
				{t("board.addTaskRow.create")}
			</button>
		</>
	);

	// ── Closed state ──────────────────────────────────────────────────────────

	if (!open) {
		if (variant === "board") {
			return (
				<button
					type="button"
					data-shortcut="add-task-trigger"
					onClick={openForm}
					className="flex w-full items-center gap-1.5 rounded-lg bg-primary/8 text-primary/80 hover:bg-primary/15 hover:text-primary px-2.5 py-1.5 text-sm font-semibold transition-all duration-150"
				>
					<Plus className="size-3" />
					{resolvedLabel}
				</button>
			);
		}
		return (
			<button
				type="button"
				data-shortcut="add-task-trigger"
				onClick={openForm}
				className="flex items-center gap-1.5 px-4 py-2.5 text-sm text-muted-foreground/70 hover:text-foreground hover:bg-muted/30 transition-all duration-150 w-full"
			>
				<Plus className="size-3" />
				{resolvedLabel}
			</button>
		);
	}

	// ── Open state: board variant ─────────────────────────────────────────────

	if (variant === "board") {
		return (
			<div className="rounded-xl border border-border/30 bg-card/50 p-2.5 shadow-sm">
				{typeSelector && (
					<div className="flex items-center gap-1.5 mb-2">{typeSelector}</div>
				)}
				<input
					ref={inputRef}
					value={value}
					onChange={(e) => setValue(e.target.value)}
					onKeyDown={(e) => {
						if (e.key === "Enter") submit();
						if (e.key === "Escape") cancel();
					}}
					placeholder={resolvedPlaceholder}
					className="w-full rounded-lg border border-border/30 bg-muted/15 px-3 py-2 text-sm font-medium outline-none placeholder:text-muted-foreground/50 focus:border-primary/40 focus:ring-2 focus:ring-primary/15 transition-all duration-150"
				/>
				<div className="mt-2 flex items-center gap-1.5 justify-end">
					{actionButtons}
				</div>
			</div>
		);
	}

	// ── Open state: list variant ──────────────────────────────────────────────

	return (
		<div className="flex flex-col gap-1.5 px-4 py-2.5 border-b border-border/20">
			<div className="flex items-center gap-2">
				{typeSelector}
				<input
					ref={inputRef}
					value={value}
					onChange={(e) => setValue(e.target.value)}
					onKeyDown={(e) => {
						if (e.key === "Enter") submit();
						if (e.key === "Escape") cancel();
					}}
					placeholder={resolvedPlaceholder}
					className="flex-1 bg-transparent text-sm font-medium outline-none placeholder:text-muted-foreground/50"
				/>
				{actionButtons}
			</div>
		</div>
	);
}
