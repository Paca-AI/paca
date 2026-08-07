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
import {
	ACTION_TYPES,
	CONDITION_NODE_TYPE,
	type PluginNodeType,
	pluginNodeTypesQueryOptions,
	TRIGGER_TYPE_GROUPS,
} from "@/lib/automation-api";

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
	if (!canEdit) return null;

	const pluginTriggers = pluginTypes?.triggers ?? [];
	const pluginConditions = pluginTypes?.conditions ?? [];
	const pluginActions = pluginTypes?.actions ?? [];

	return (
		<div className="flex items-center gap-2">
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
						{pluginConditions.length > 0 && (
							<DropdownMenuLabel>
								{t("automation.palette.builtIn")}
							</DropdownMenuLabel>
						)}
						<DropdownMenuItem
							onClick={() => onAddCondition(CONDITION_NODE_TYPE)}
						>
							{t("automation.nodeKind.condition")}
						</DropdownMenuItem>
					</DropdownMenuGroup>
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
						{pluginActions.length > 0 && (
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
					{pluginActions.length > 0 && (
						<PluginTypeGroup items={pluginActions} onSelect={onAddAction} />
					)}
				</DropdownMenuContent>
			</DropdownMenu>
		</div>
	);
}

// Shared "Plugins" section appended below the built-ins in each dropdown.
function PluginTypeGroup({
	items,
	onSelect,
}: {
	items: PluginNodeType[];
	onSelect: (type: string) => void;
}) {
	const { t } = useTranslation("projects");
	return (
		<>
			<DropdownMenuSeparator />
			<DropdownMenuGroup>
				<DropdownMenuLabel>{t("automation.palette.plugins")}</DropdownMenuLabel>
				{items.map((item) => (
					<DropdownMenuItem key={item.type} onClick={() => onSelect(item.type)}>
						{item.label}
					</DropdownMenuItem>
				))}
			</DropdownMenuGroup>
		</>
	);
}
