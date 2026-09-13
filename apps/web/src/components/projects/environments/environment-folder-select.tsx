import { Plus } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectSeparator,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import type { Environment } from "@/lib/environment-api";
import { EnvironmentCreateDialog } from "./environment-create-dialog";
import { FolderCreateDialog } from "./folder-create-dialog";

// DefaultEnvironmentSelect / DefaultFolderSelect are the shared "pick a
// default environment/folder for this agent" pair — one implementation
// instead of the three that used to exist independently (create-agent-
// dialog.tsx's LLM and provider_cli forms, agent-detail.tsx's OverviewTab),
// which had drifted: only the OverviewTab copy offered a "create new
// environment/folder" escape hatch, the other two silently required the
// project to already have one.
//
// Deliberately NOT unified with agent-picker.tsx's EnvironmentPickerInline/
// FolderPickerInline — those back a different concept (a per-conversation
// override in the chat composer, pill-styled, worded "temporary" instead
// of "no default", and auto-selected from the agent's own default via
// effects) rather than a settings-form "default" field. They do reuse the
// same EnvironmentCreateDialog/FolderCreateDialog underneath, same as here.
//
// Callers keep owning their own environmentId/folderId state (and the
// "picking a new environment clears the previously-picked folder" rule —
// a folder only ever belongs to one environment) since that state also
// feeds each caller's own dirty-checking/save-payload logic; only the
// Select markup and create-dialog wiring are shared here.

const NO_ENVIRONMENT = "__none__";
const NO_FOLDER = "__none__";
const CREATE_NEW_ENVIRONMENT = "__create_environment__";
const CREATE_NEW_FOLDER = "__create_folder__";

export function DefaultEnvironmentSelect({
	projectId,
	environments,
	value,
	onChange,
	required = false,
	disabled,
	className,
	triggerSize,
}: {
	projectId: string;
	environments: Environment[];
	value: string;
	onChange: (id: string) => void;
	/** Omits the "no default environment" option and shows a placeholder
	 * instead — for agent types (provider_cli) that require one. */
	required?: boolean;
	disabled?: boolean;
	className?: string;
	triggerSize?: "sm" | "default";
}) {
	const { t } = useTranslation("projects");
	const [createOpen, setCreateOpen] = useState(false);

	return (
		<>
			<Select
				value={required ? value : value || NO_ENVIRONMENT}
				onValueChange={(v) => {
					if (!v) return;
					if (v === CREATE_NEW_ENVIRONMENT) {
						setCreateOpen(true);
						return;
					}
					onChange(!required && v === NO_ENVIRONMENT ? "" : v);
				}}
				items={[
					...(required
						? []
						: [
								{
									value: NO_ENVIRONMENT,
									label: t("agents.detail.overview.noDefaultEnvironment"),
								},
							]),
					...environments.map((env) => ({ value: env.id, label: env.name })),
					{
						value: CREATE_NEW_ENVIRONMENT,
						label: t("environments.picker.createNew"),
					},
				]}
				disabled={disabled}
			>
				<SelectTrigger className={className} size={triggerSize}>
					<SelectValue
						placeholder={
							required
								? t("agents.createDialog.environmentPlaceholder")
								: undefined
						}
					/>
				</SelectTrigger>
				<SelectContent>
					{!required && (
						<>
							<SelectItem value={NO_ENVIRONMENT}>
								{t("agents.detail.overview.noDefaultEnvironment")}
							</SelectItem>
							<SelectSeparator />
						</>
					)}
					{environments.map((env) => (
						<SelectItem key={env.id} value={env.id}>
							{env.name}
						</SelectItem>
					))}
					<SelectSeparator />
					<SelectItem value={CREATE_NEW_ENVIRONMENT}>
						<Plus className="size-3.5" />
						{t("environments.picker.createNew")}
					</SelectItem>
				</SelectContent>
			</Select>
			<EnvironmentCreateDialog
				projectId={projectId}
				open={createOpen}
				onOpenChange={setCreateOpen}
				onCreated={(env) => onChange(env.id)}
			/>
		</>
	);
}

export function DefaultFolderSelect({
	projectId,
	environment,
	value,
	onChange,
	disabled,
	className,
	triggerSize,
}: {
	projectId: string;
	/** The currently-selected default environment — the folder select has
	 * nothing to offer (and isn't rendered by any caller) without one. */
	environment: Environment;
	value: string;
	onChange: (id: string) => void;
	disabled?: boolean;
	className?: string;
	triggerSize?: "sm" | "default";
}) {
	const { t } = useTranslation("projects");
	const [createOpen, setCreateOpen] = useState(false);
	// Defensive fallback, not just belt-and-suspenders: a Go nil slice
	// serializes to JSON `null` (not `[]`) unless explicitly initialized,
	// so a freshly-created environment's `folders` can legitimately arrive
	// here as null/undefined rather than an empty array.
	const folders = environment.folders ?? [];

	return (
		<>
			<Select
				value={value || NO_FOLDER}
				onValueChange={(v) => {
					if (!v) return;
					if (v === CREATE_NEW_FOLDER) {
						setCreateOpen(true);
						return;
					}
					onChange(v === NO_FOLDER ? "" : v);
				}}
				items={[
					{
						value: NO_FOLDER,
						label: t("agents.detail.overview.noDefaultFolder"),
					},
					...folders.map((folder) => ({
						value: folder.id,
						label: folder.path,
					})),
					{
						value: CREATE_NEW_FOLDER,
						label: t("environments.picker.folderCreateNew"),
					},
				]}
				disabled={disabled}
			>
				<SelectTrigger className={className} size={triggerSize}>
					<SelectValue />
				</SelectTrigger>
				<SelectContent>
					<SelectItem value={NO_FOLDER}>
						{t("agents.detail.overview.noDefaultFolder")}
					</SelectItem>
					{folders.length > 0 && <SelectSeparator />}
					{folders.map((folder) => (
						<SelectItem key={folder.id} value={folder.id}>
							{folder.path}
						</SelectItem>
					))}
					<SelectSeparator />
					<SelectItem value={CREATE_NEW_FOLDER}>
						<Plus className="size-3.5" />
						{t("environments.picker.folderCreateNew")}
					</SelectItem>
				</SelectContent>
			</Select>
			<FolderCreateDialog
				projectId={projectId}
				environmentId={environment.id}
				environmentStatus={environment.status}
				open={createOpen}
				onOpenChange={setCreateOpen}
				onCreated={(folder) => onChange(folder.id)}
			/>
		</>
	);
}
