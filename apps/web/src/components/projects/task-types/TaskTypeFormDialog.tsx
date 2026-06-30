import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
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
import { Textarea } from "@/components/ui/textarea";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import {
	createTaskType,
	type TaskType,
	taskTypesQueryOptions,
	updateTaskType,
} from "@/lib/project-api";
import { getTaskTypeIconComponent, TASK_TYPE_ICONS } from "./task-type-icons";

interface TaskTypeFormDialogProps {
	projectId: string;
	taskType?: TaskType;
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

export function TaskTypeFormDialog({
	projectId,
	taskType,
	open,
	onOpenChange,
}: TaskTypeFormDialogProps) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const isEdit = !!taskType;

	const [name, setName] = useState(taskType?.name ?? "");
	const [icon, setIcon] = useState<string>(taskType?.icon ?? "");
	const [color, setColor] = useState<string>(taskType?.color ?? "#6366f1");
	const [description, setDescription] = useState<string>(
		taskType?.description ?? "",
	);
	const [nameError, setNameError] = useState<string | null>(null);
	const [error, setError] = useState<string | null>(null);

	const reset = () => {
		setName(taskType?.name ?? "");
		setIcon(taskType?.icon ?? "");
		setColor(taskType?.color ?? "#6366f1");
		setDescription(taskType?.description ?? "");
		setNameError(null);
		setError(null);
	};

	const mutation = useMutation({
		mutationFn: () => {
			const payload = {
				name: name.trim(),
				icon: icon.trim() || null,
				color: color || null,
				description: description.trim() || null,
			};
			if (isEdit && taskType) {
				return updateTaskType(projectId, taskType.id, payload);
			}
			return createTaskType(projectId, payload);
		},
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: taskTypesQueryOptions(projectId).queryKey,
			});
			onOpenChange(false);
			reset();
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			if (
				code === ApiErrorCode.TaskTypeNameInvalid ||
				code === ApiErrorCode.BadRequest
			) {
				setNameError(t("project.taskTypes.errNameInvalid"));
				return;
			}
			setError(t("project.taskTypes.errSaveFailed"));
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
					<DialogTitle>
						{isEdit
							? t("project.taskTypes.edit")
							: t("project.taskTypes.create")}
					</DialogTitle>
					<DialogDescription>
						{isEdit
							? t("project.taskTypes.editDesc")
							: t("project.taskTypes.createDesc")}
					</DialogDescription>
				</DialogHeader>

				<div className="space-y-4 py-1">
					{/* Name */}
					<div className="space-y-1.5">
						<Label htmlFor="type-name">{t("project.taskTypes.name")}</Label>
						<Input
							id="type-name"
							value={name}
							onChange={(e) => {
								setName(e.target.value);
								setNameError(null);
							}}
							onKeyDown={(e) => {
								if (e.key === "Enter" && canSubmit) mutation.mutate();
							}}
							placeholder={t("project.taskTypes.namePlaceholder")}
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

					{/* Icon */}
					<div className="space-y-1.5">
						<Label>
							{t("project.taskTypes.icon")}{" "}
							<span className="text-muted-foreground font-normal">
								{t("project.taskTypes.optional")}
							</span>
						</Label>
						<div className="flex flex-wrap gap-1">
							{TASK_TYPE_ICONS.map(({ name, component: Icon }) => {
								const iconLabel = t(`project.taskTypes.icons.${name}`);
								return (
								<button
									key={name}
									type="button"
									title={iconLabel}
									aria-label={iconLabel}
									className={`flex size-8 items-center justify-center rounded-md border transition-colors ${
										icon === name
											? "border-foreground bg-accent"
											: "border-transparent hover:border-border hover:bg-accent/60"
									}`}
									onClick={() => setIcon(icon === name ? "" : name)}
								>
									<Icon className="size-4" />
								</button>
								);
							})}
						</div>
						{icon ? (
							<div className="flex items-center gap-1.5 text-xs text-muted-foreground">
								{(() => {
									const Comp = getTaskTypeIconComponent(icon);
									return Comp ? (
										<>
											<Comp className="size-3.5" />
											<span>{icon}</span>
										</>
									) : null;
								})()}
								<button
									type="button"
									className="ml-1 underline hover:text-foreground"
									onClick={() => setIcon("")}
								>
									{t("project.taskTypes.clear")}
								</button>
							</div>
						) : null}
					</div>

					{/* Color */}
					<div className="space-y-1.5">
						<Label>{t("project.taskTypes.color")}</Label>
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
								title={t("project.taskTypes.customColor")}
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

					{/* Description */}
					<div className="space-y-1.5">
						<Label htmlFor="type-description">
							{t("project.taskTypes.description")}{" "}
							<span className="text-muted-foreground font-normal">
								{t("project.taskTypes.optional")}
							</span>
						</Label>
						<Textarea
							id="type-description"
							value={description}
							onChange={(e) => setDescription(e.target.value)}
							placeholder={t("project.taskTypes.descPlaceholder")}
							rows={2}
							className="resize-none"
						/>
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
						{t("project.taskTypes.cancel")}
					</DialogClose>
					<Button
						size="sm"
						disabled={!canSubmit}
						onClick={() => mutation.mutate()}
					>
						{mutation.isPending ? (
							<Loader2 className="size-3.5 animate-spin" />
						) : null}
						{isEdit
							? t("project.taskTypes.saveChanges")
							: t("project.taskTypes.createType")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
