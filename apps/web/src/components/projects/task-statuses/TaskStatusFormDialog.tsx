import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogClose,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import {
	createTaskStatus,
	STATUS_CATEGORIES,
	STATUS_CATEGORY_LABELS,
	type StatusCategory,
	type TaskStatus,
	taskStatusesQueryOptions,
	updateTaskStatus,
} from "@/lib/project-api";

interface TaskStatusFormDialogProps {
	projectId: string;
	status?: TaskStatus;
	defaultPosition?: number;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

const COLOR_PRESETS = [
	"#6366f1",
	"#8b5cf6",
	"#ec4899",
	"#ef4444",
	"#f97316",
	"#eab308",
	"#22c55e",
	"#14b8a6",
	"#06b6d4",
	"#3b82f6",
	"#64748b",
	"#78716c",
];

export function TaskStatusFormDialog({
	projectId,
	status,
	defaultPosition = 0,
	open,
	onOpenChange,
}: TaskStatusFormDialogProps) {
	const queryClient = useQueryClient();
	const isEdit = !!status;

	const [name, setName] = useState(status?.name ?? "");
	const [category, setCategory] = useState<StatusCategory>(
		status?.category ?? "todo",
	);
	const [color, setColor] = useState<string>(status?.color ?? "#6366f1");
	const [nameError, setNameError] = useState<string | null>(null);
	const [error, setError] = useState<string | null>(null);

	const reset = () => {
		setName(status?.name ?? "");
		setCategory(status?.category ?? "todo");
		setColor(status?.color ?? "#6366f1");
		setNameError(null);
		setError(null);
	};

	const mutation = useMutation({
		mutationFn: () => {
			if (isEdit && status) {
				return updateTaskStatus(projectId, status.id, {
					name: name.trim(),
					color: color || null,
					category,
				});
			}
			return createTaskStatus(projectId, {
				name: name.trim(),
				color: color || null,
				position: defaultPosition,
				category,
			});
		},
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: taskStatusesQueryOptions(projectId).queryKey,
			});
			onOpenChange(false);
			reset();
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			if (
				code === ApiErrorCode.TaskStatusNameInvalid ||
				code === ApiErrorCode.BadRequest
			) {
				setNameError("Status name is empty or invalid.");
				return;
			}
			if (code === ApiErrorCode.TaskStatusCategoryInvalid) {
				setError("Invalid category selected.");
				return;
			}
			setError("Failed to save status. Please try again.");
		},
	});

	const canSubmit = name.trim().length > 0 && !mutation.isPending;

	return (
		<Dialog
			open={open}
			onOpenChange={(o) => {
				onOpenChange(o);
				if (!o) reset();
			}}
		>
			<DialogContent className="sm:max-w-sm">
				<DialogHeader>
					<DialogTitle>{isEdit ? "Edit status" : "Create status"}</DialogTitle>
					<DialogDescription>
						{isEdit
							? "Update this workflow status."
							: "Add a new status to the project workflow."}
					</DialogDescription>
				</DialogHeader>

				<div className="space-y-4 py-1">
					{/* Name */}
					<div className="space-y-1.5">
						<Label htmlFor="status-name">Name</Label>
						<Input
							id="status-name"
							value={name}
							onChange={(e) => {
								setName(e.target.value);
								setNameError(null);
							}}
							onKeyDown={(e) => {
								if (e.key === "Enter" && canSubmit) mutation.mutate();
							}}
							placeholder="e.g. In Review"
							autoFocus
							className={
								nameError
									? "border-destructive focus-visible:ring-destructive/30"
									: ""
							}
						/>
						{nameError ? (
							<p className="text-xs text-destructive">{nameError}</p>
						) : null}
					</div>

					{/* Category */}
					<div className="space-y-1.5">
						<Label htmlFor="status-category">Category</Label>
						<Select
							value={category}
							onValueChange={(v) => setCategory(v as StatusCategory)}
						>
							<SelectTrigger id="status-category" className="w-full">
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								{STATUS_CATEGORIES.map((cat) => (
									<SelectItem key={cat} value={cat}>
										{STATUS_CATEGORY_LABELS[cat]}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					</div>

					{/* Color */}
					<div className="space-y-1.5">
						<Label>Color</Label>
						<div className="flex items-center gap-2 flex-wrap">
							{COLOR_PRESETS.map((preset) => (
								<button
									key={preset}
									type="button"
									className={`size-6 rounded-full border-2 transition-transform hover:scale-110 ${
										color === preset
											? "border-foreground scale-110"
											: "border-transparent"
									}`}
									style={{ backgroundColor: preset }}
									onClick={() => setColor(preset)}
									aria-label={preset}
								/>
							))}
							<label
								title="Custom color"
								className={`relative size-6 rounded-full cursor-pointer border-2 transition-transform hover:scale-110 overflow-hidden shrink-0 ${
									!COLOR_PRESETS.includes(color)
										? "border-foreground scale-110"
										: "border-transparent"
								}`}
								style={{
									background:
										"conic-gradient(#ef4444, #f97316, #eab308, #22c55e, #14b8a6, #06b6d4, #3b82f6, #6366f1, #8b5cf6, #ec4899, #ef4444)",
									backgroundSize: "120% 120%",
									backgroundPosition: "center",
								}}
							>
								<input
									type="color"
									value={color}
									onChange={(e) => setColor(e.target.value)}
									className="sr-only"
								/>
							</label>
						</div>
					</div>
				</div>

				{error ? (
					<p className="text-xs text-destructive bg-destructive/10 rounded-lg px-3 py-2">
						{error}
					</p>
				) : null}

				<DialogFooter>
					<DialogClose
						render={
							<Button
								variant="outline"
								size="sm"
								disabled={mutation.isPending}
							/>
						}
					>
						Cancel
					</DialogClose>
					<Button
						size="sm"
						disabled={!canSubmit}
						onClick={() => mutation.mutate()}
					>
						{mutation.isPending ? (
							<Loader2 className="size-3.5 animate-spin" />
						) : null}
						{isEdit ? "Save changes" : "Create status"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
