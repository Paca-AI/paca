import { useState } from "react";
import { useTranslation } from "react-i18next";
import { getTaskTypeIconComponent } from "@/components/projects/task-types/task-type-icons";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import type { TaskType } from "@/lib/project-api";
import { SearchableList } from "./task-detail/property-field/searchable-list";

interface TaskTypeSelectorProps {
	taskTypes: TaskType[];
	value: string | null | undefined;
	onChange?: (taskTypeId: string) => void;
	canEdit?: boolean;
	align?: "start" | "end" | "center";
}

/** Badge + dropdown for picking a task's type. Used anywhere a task type can be set or changed. */
export function TaskTypeSelector({
	taskTypes,
	value,
	onChange,
	canEdit = true,
	align = "start",
}: TaskTypeSelectorProps) {
	const { t } = useTranslation("projects");
	const [open, setOpen] = useState(false);
	const taskType = taskTypes.find((tt) => tt.id === value);
	const Icon = taskType ? getTaskTypeIconComponent(taskType.icon) : null;

	const badgeStyle = taskType
		? {
				borderColor: taskType.color ? `${taskType.color}44` : "var(--border)",
				backgroundColor: taskType.color
					? `${taskType.color}15`
					: "var(--muted)",
				color: taskType.color ?? "inherit",
			}
		: undefined;

	const badgeContent = taskType ? (
		<>
			{Icon && <Icon className="size-3 shrink-0" />}
			<span className="truncate">{taskType.name}</span>
		</>
	) : (
		<span className="text-muted-foreground/50">
			{t("board.taskTypeSelector.noType")}
		</span>
	);

	if (!canEdit || taskTypes.length === 0) {
		return (
			<span
				className="inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-bold leading-tight tracking-wide border truncate max-w-full"
				style={badgeStyle}
			>
				{badgeContent}
			</span>
		);
	}

	return (
		<Popover open={open} onOpenChange={setOpen}>
			<PopoverTrigger
				type="button"
				className="inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-bold leading-tight tracking-wide border truncate max-w-full hover:opacity-80 transition-opacity"
				style={badgeStyle}
			>
				{badgeContent}
			</PopoverTrigger>
			<PopoverContent
				className="w-44 p-1 rounded-xl border border-border/40 shadow-lg"
				align={align}
			>
				<SearchableList
					items={taskTypes.map((tt) => {
						const TtIcon = getTaskTypeIconComponent(tt.icon);
						return {
							key: tt.id,
							label: tt.name,
							selected: tt.id === value,
							onSelect: () => {
								onChange?.(tt.id);
								setOpen(false);
							},
							content: (
								<>
									{TtIcon && (
										<TtIcon
											className="size-3.5 shrink-0"
											style={tt.color ? { color: tt.color } : undefined}
										/>
									)}
									<span className="flex-1 text-left truncate">{tt.name}</span>
								</>
							),
						};
					})}
				/>
			</PopoverContent>
		</Popover>
	);
}
