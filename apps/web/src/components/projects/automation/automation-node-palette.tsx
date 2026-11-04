import { useQuery } from "@tanstack/react-query";
import { GitBranch, Plus, Zap } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuGroup,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useJevEnabled } from "@/hooks/use-jev-enabled";
import {
	ACTION_TYPES,
	CONDITION_NODE_TYPE,
	JEV_CONDITION_NODE_TYPE,
	type PluginNodeType,
	pluginNodeTypesQueryOptions,
	TRIGGER_TYPE_GROUPS,
} from "@/lib/automation-api";
import {
	gitNodeShortLabel,
	isGitNodeType,
} from "@/lib/automation-plugin-config";

interface AutomationNodePaletteProps {
	projectId: string;
	canEdit: boolean;
	onAddTrigger: (type: string) => void;
	onAddCondition: (type: string) => void;
	onAddAction: (type: string) => void;
}

export function AutomationNodePalette({
	projectId,
	canEdit,
	onAddTrigger,
	onAddCondition,
	onAddAction,
}: AutomationNodePaletteProps) {
	const { t } = useTranslation("projects");
	const { data: pluginTypes } = useQuery(
		pluginNodeTypesQueryOptions(projectId),
	);
	const jevEnabled = useJevEnabled(projectId);
	if (!canEdit) return null;

	// Core-owned Git node types arrive in the same lists as plugin ones —
	// they get their own section, not "Plugins".
	const gitTriggers = (pluginTypes?.triggers ?? []).filter(isGitNodeType);
	const gitConditions = (pluginTypes?.conditions ?? []).filter(isGitNodeType);
	const gitActions = (pluginTypes?.actions ?? []).filter(isGitNodeType);
	const pluginTriggers = (pluginTypes?.triggers ?? []).filter(
		(n) => !isGitNodeType(n),
	);
	const pluginConditions = (pluginTypes?.conditions ?? []).filter(
		(n) => !isGitNodeType(n),
	);
	const pluginActions = (pluginTypes?.actions ?? []).filter(
		(n) => !isGitNodeType(n),
	);

	return (
		<div className="flex flex-wrap items-center gap-2 py-2">
			<DropdownMenu>
				<DropdownMenuTrigger
					render={
						<Button variant="outline" size="sm" className="gap-1.5">
							<Zap className="size-3.5" />
							{t("automation.palette.addTrigger")}
						</Button>
					}
				/>
				<DropdownMenuContent align="start" className="w-56">
					{TRIGGER_TYPE_GROUPS.map((group, i) => (
						<DropdownMenuGroup key={group.labelKey}>
							{i > 0 && <DropdownMenuSeparator />}
							<DropdownMenuLabel>
								{t(`automation.palette.triggerGroups.${group.labelKey}`)}
							</DropdownMenuLabel>
							{group.types.map((type) => (
								<DropdownMenuItem key={type} onClick={() => onAddTrigger(type)}>
									{t(`automation.triggerTypes.${type}`)}
								</DropdownMenuItem>
							))}
						</DropdownMenuGroup>
					))}
					{gitTriggers.length > 0 && (
						<PluginTypeGroup items={gitTriggers} onSelect={onAddTrigger} git />
					)}
					{pluginTriggers.length > 0 && (
						<PluginTypeGroup items={pluginTriggers} onSelect={onAddTrigger} />
					)}
				</DropdownMenuContent>
			</DropdownMenu>

			{/* The built-in N-branch switch is the only "built-in" condition node
			type — plugin-contributed conditions are their own separate
			condition node type, listed in a "Plugins" section below it,
			exactly like plugin triggers/actions. */}
			<DropdownMenu>
				<DropdownMenuTrigger
					render={
						<Button variant="outline" size="sm" className="gap-1.5">
							<GitBranch className="size-3.5" />
							{t("automation.palette.addCondition")}
						</Button>
					}
				/>
				<DropdownMenuContent align="start" className="w-56">
					<DropdownMenuGroup>
						{gitConditions.length + pluginConditions.length > 0 && (
							<DropdownMenuLabel>
								{t("automation.palette.builtIn")}
							</DropdownMenuLabel>
						)}
						<DropdownMenuItem
							onClick={() => onAddCondition(CONDITION_NODE_TYPE)}
						>
							{t("automation.nodeKind.condition")}
						</DropdownMenuItem>
						{jevEnabled && (
							<DropdownMenuItem
								onClick={() => onAddCondition(JEV_CONDITION_NODE_TYPE)}
							>
								{t("automation.conditionTypes.jev_condition")}
							</DropdownMenuItem>
						)}
					</DropdownMenuGroup>
					{gitConditions.length > 0 && (
						<PluginTypeGroup
							items={gitConditions}
							onSelect={onAddCondition}
							git
						/>
					)}
					{pluginConditions.length > 0 && (
						<PluginTypeGroup
							items={pluginConditions}
							onSelect={onAddCondition}
						/>
					)}
				</DropdownMenuContent>
			</DropdownMenu>

			<DropdownMenu>
				<DropdownMenuTrigger
					render={
						<Button variant="outline" size="sm" className="gap-1.5">
							<Plus className="size-3.5" />
							{t("automation.palette.addAction")}
						</Button>
					}
				/>
				<DropdownMenuContent align="start" className="w-56">
					<DropdownMenuGroup>
						{gitActions.length + pluginActions.length > 0 && (
							<DropdownMenuLabel>
								{t("automation.palette.builtIn")}
							</DropdownMenuLabel>
						)}
						{ACTION_TYPES.map((type) => (
							<DropdownMenuItem key={type} onClick={() => onAddAction(type)}>
								{t(`automation.actionTypes.${type}`)}
							</DropdownMenuItem>
						))}
					</DropdownMenuGroup>
					{gitActions.length > 0 && (
						<PluginTypeGroup items={gitActions} onSelect={onAddAction} git />
					)}
					{pluginActions.length > 0 && (
						<PluginTypeGroup items={pluginActions} onSelect={onAddAction} />
					)}
				</DropdownMenuContent>
			</DropdownMenu>
		</div>
	);
}

// Shared "Plugins" / "Git" section appended below the built-ins in each dropdown.
function PluginTypeGroup({
	items,
	onSelect,
	git = false,
}: {
	items: PluginNodeType[];
	onSelect: (type: string) => void;
	git?: boolean;
}) {
	const { t } = useTranslation("projects");
	return (
		<>
			<DropdownMenuSeparator />
			<DropdownMenuGroup>
				<DropdownMenuLabel>
					{git ? t("automation.palette.git") : t("automation.palette.plugins")}
				</DropdownMenuLabel>
				{items.map((item) => (
					<DropdownMenuItem key={item.type} onClick={() => onSelect(item.type)}>
						{git ? gitNodeShortLabel(item.label) : item.label}
					</DropdownMenuItem>
				))}
			</DropdownMenuGroup>
		</>
	);
}
