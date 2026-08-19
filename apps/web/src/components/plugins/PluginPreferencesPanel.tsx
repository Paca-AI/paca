import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Eye, EyeOff, GripVertical } from "lucide-react";
import { type DragEvent, useCallback, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import {
	type ExtensionPointId,
	type PluginRegistration,
	pluginsQueryOptions,
	updatePluginExtensionSetting,
} from "@/lib/plugin-api";
import { usePluginRegistry } from "@/lib/plugins/registry";

const EXTENSION_POINT_LABEL_KEYS = {
	"sidebar.general.section": "preferences.extensionPoints.sidebarGeneral",
	"sidebar.project.section": "preferences.extensionPoints.sidebarProject",
	"task.detail.section": "preferences.extensionPoints.taskDetail",
	"project.settings.tab": "preferences.extensionPoints.projectSettingsTab",
	view: "preferences.extensionPoints.customView",
	"project.page": "preferences.extensionPoints.projectPage",
	"admin.page": "preferences.extensionPoints.adminPage",
	"user.settings.tab": "preferences.extensionPoints.userSettingsTab",
} as const satisfies Record<ExtensionPointId, string>;

const ALL_POINTS: ExtensionPointId[] = [
	"sidebar.general.section",
	"sidebar.project.section",
	"task.detail.section",
	"project.settings.tab",
	"view",
	"project.page",
	"admin.page",
	"user.settings.tab",
];

interface DraggableItemProps {
	reg: PluginRegistration;
	point: ExtensionPointId;
	index: number;
	total: number;
	onToggleHidden: (reg: PluginRegistration) => void;
	onDragStart: (e: DragEvent<HTMLLIElement>, index: number) => void;
	onDragOver: (e: DragEvent<HTMLLIElement>, index: number) => void;
	onDrop: (index: number) => void;
	dragOverIndex: number | null;
}

function DraggableItem({
	reg,
	point,
	index,
	onToggleHidden,
	onDragStart,
	onDragOver,
	onDrop,
	dragOverIndex,
}: DraggableItemProps) {
	const { t } = useTranslation("plugins");
	return (
		<li
			draggable
			onDragStart={(e) => onDragStart(e, index)}
			onDragOver={(e) => onDragOver(e, index)}
			onDrop={() => onDrop(index)}
			onDragEnd={() => onDrop(-1)}
			className={`flex items-center gap-2 rounded-lg border px-3 py-2 bg-background transition-colors ${
				dragOverIndex === index
					? "border-primary/50 bg-primary/5"
					: "border-border/40 hover:border-border/70"
			}`}
		>
			<GripVertical className="size-4 text-muted-foreground/40 shrink-0 cursor-grab active:cursor-grabbing" />
			<div className="flex-1 min-w-0">
				<p className="text-sm font-medium truncate">{reg.label}</p>
				<p className="text-xs text-muted-foreground truncate">
					{reg.pluginName} · {t(EXTENSION_POINT_LABEL_KEYS[point])}
				</p>
			</div>
			<button
				type="button"
				onClick={() => onToggleHidden(reg)}
				title={
					reg.hidden
						? t("preferences.actions.show")
						: t("preferences.actions.hide")
				}
				className="flex items-center justify-center size-7 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors shrink-0"
			>
				{reg.hidden ? (
					<EyeOff className="size-3.5" />
				) : (
					<Eye className="size-3.5" />
				)}
			</button>
		</li>
	);
}

interface PointSectionProps {
	point: ExtensionPointId;
	registrations: PluginRegistration[];
	onToggleHidden: (point: ExtensionPointId, reg: PluginRegistration) => void;
	onReorder: (
		point: ExtensionPointId,
		fromIndex: number,
		toIndex: number,
	) => void;
}

function PointSection({
	point,
	registrations,
	onToggleHidden,
	onReorder,
}: PointSectionProps) {
	const { t } = useTranslation("plugins");
	const [dragOverIndex, setDragOverIndex] = useState<number | null>(null);
	const dragIndexRef = useRef<number>(-1);

	const handleDragStart = (_e: DragEvent<HTMLLIElement>, index: number) => {
		dragIndexRef.current = index;
	};

	const handleDragOver = (e: DragEvent<HTMLLIElement>, index: number) => {
		e.preventDefault();
		setDragOverIndex(index);
	};

	const handleDrop = (toIndex: number) => {
		const fromIndex = dragIndexRef.current;
		setDragOverIndex(null);
		dragIndexRef.current = -1;
		if (toIndex === -1 || fromIndex === toIndex) return;
		onReorder(point, fromIndex, toIndex);
	};

	if (registrations.length === 0) return null;

	return (
		<div className="space-y-2">
			<p className="text-xs font-semibold uppercase tracking-widest text-muted-foreground/60">
				{t(EXTENSION_POINT_LABEL_KEYS[point])}
			</p>
			<div className="space-y-1.5">
				{registrations.map((reg, index) => (
					<DraggableItem
						key={`${reg.pluginId}-${reg.component}`}
						reg={reg}
						point={point}
						index={index}
						total={registrations.length}
						onToggleHidden={(r) => onToggleHidden(point, r)}
						onDragStart={handleDragStart}
						onDragOver={handleDragOver}
						onDrop={handleDrop}
						dragOverIndex={dragOverIndex}
					/>
				))}
			</div>
		</div>
	);
}

export function PluginPreferencesPanel() {
	const { t } = useTranslation("plugins");
	const qc = useQueryClient();
	const { data: plugins = [] } = useQuery(pluginsQueryOptions);
	const { getRegistrations } = usePluginRegistry();

	const mutation = useMutation({
		mutationFn: updatePluginExtensionSetting,
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["plugins"] });
		},
	});

	const handleToggleHidden = useCallback(
		(point: ExtensionPointId, reg: PluginRegistration) => {
			mutation.mutate({
				plugin_id: reg.pluginUUID, // Use UUID for API call
				extension_point: point,
				settings: { hidden: !reg.hidden, order: reg.order },
			});
		},
		[mutation],
	);

	const handleReorder = useCallback(
		(point: ExtensionPointId, fromIndex: number, toIndex: number) => {
			const regs = getRegistrations(point);
			const reordered = [...regs];
			const [moved] = reordered.splice(fromIndex, 1);
			reordered.splice(toIndex, 0, moved);

			// Persist the new order for each registration that changed position
			for (let i = 0; i < reordered.length; i++) {
				const reg = reordered[i];
				const nextOrder = i + 1;
				if (reg.order !== nextOrder || reg === moved) {
					mutation.mutate({
						plugin_id: reg.pluginUUID, // Use UUID for API call
						extension_point: point,
						settings: { hidden: reg.hidden ?? false, order: nextOrder },
					});
				}
			}
		},
		[getRegistrations, mutation],
	);

	const hasAny = plugins.some(
		(p) => p.enabled && p.manifest.frontend?.extensionPoints?.length,
	);

	if (!hasAny) {
		return (
			<div className="py-8 text-center text-sm text-muted-foreground">
				{t("preferences.empty")}
			</div>
		);
	}

	return (
		<div className="space-y-6">
			{ALL_POINTS.map((point) => {
				const regs = getRegistrations(point);
				if (regs.length === 0) return null;
				return (
					<PointSection
						key={point}
						point={point}
						registrations={regs}
						onToggleHidden={handleToggleHidden}
						onReorder={handleReorder}
					/>
				);
			})}
		</div>
	);
}
