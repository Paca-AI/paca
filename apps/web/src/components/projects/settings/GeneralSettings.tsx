import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { AvatarUpload } from "@/components/shared/avatar-upload";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { projectQueryOptions, updateProject } from "@/lib/project-api";

export function GeneralSettings({
	projectId,
	canEdit,
}: {
	projectId: string;
	canEdit: boolean;
}) {
	const { t } = useTranslation("projects");
	const queryClient = useQueryClient();
	const { data: project } = useQuery(projectQueryOptions(projectId));

	const [name, setName] = useState(project?.name ?? "");
	const [description, setDescription] = useState(project?.description ?? "");
	const [prefix, setPrefix] = useState(project?.task_id_prefix ?? "");
	const [nameError, setNameError] = useState<string | null>(null);
	const [prefixError, setPrefixError] = useState<string | null>(null);
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		meta: { success: "updated", subject: "projectInfo" },
		mutationFn: () =>
			updateProject(projectId, {
				name: name.trim(),
				description: description.trim(),
				task_id_prefix: prefix.trim() || undefined,
			}),
		onSuccess: async (updated) => {
			await queryClient.invalidateQueries({
				queryKey: projectQueryOptions(projectId).queryKey,
			});
			// Also update the projects list cache
			await queryClient.invalidateQueries({ queryKey: ["projects"] });
			setName(updated.name);
			setDescription(updated.description);
			setPrefix(updated.task_id_prefix);
			setError(null);
			setNameError(null);
			setPrefixError(null);
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			if (code === ApiErrorCode.ProjectNameTaken) {
				setNameError(t("settings.general.errors.nameTaken"));
				return;
			}
			if (code === ApiErrorCode.ProjectNameInvalid) {
				setNameError(t("settings.general.errors.nameInvalid"));
				return;
			}
			if (code === ApiErrorCode.ProjectPrefixInvalid) {
				setPrefixError(t("settings.general.errors.prefixInvalid"));
				return;
			}
			setError(t("settings.general.errors.updateFailed"));
		},
	});

	const isDirty =
		name.trim() !== (project?.name ?? "") ||
		description.trim() !== (project?.description ?? "") ||
		prefix.trim() !== (project?.task_id_prefix ?? "");

	return (
		<div className="rounded-xl border border-border/60 bg-card p-6">
			<h3 className="font-[Syne] text-base font-semibold mb-4">
				{t("settings.general.title")}
			</h3>

			{/* Identity header — avatar next to the project's name/prefix, same
			    layout as the profile page's own avatar setting. */}
			<div className="flex items-center gap-4 mb-6">
				<AvatarUpload
					basePath={`/projects/${projectId}`}
					avatar={{
						name: project?.name ?? "",
						avatarUrl: project?.avatar_url,
						size: "xl",
					}}
					disabled={!canEdit}
					className="size-14 rounded-xl"
					labels={{
						change: t("settings.general.avatar.change"),
						remove: t("settings.general.avatar.remove"),
						uploading: t("settings.general.avatar.uploading"),
						invalidType: t("settings.general.avatar.errors.invalidType"),
						tooLarge: t("settings.general.avatar.errors.tooLarge"),
						uploadFailed: t("settings.general.avatar.errors.uploadFailed"),
						removeFailed: t("settings.general.avatar.errors.removeFailed"),
					}}
					onChange={(result) => {
						queryClient.setQueryData(
							projectQueryOptions(projectId).queryKey,
							(old) => (old ? { ...old, ...result } : old),
						);
						// Other places reading the project list (sidebar switcher,
						// home page cards) hold a separate cache entry that
						// setQueryData above doesn't touch.
						queryClient.invalidateQueries({ queryKey: ["projects"] });
					}}
				/>
				<div>
					<p className="text-lg font-semibold leading-tight">{project?.name}</p>
					{project?.task_id_prefix ? (
						<p className="mt-0.5 font-[JetBrains_Mono,monospace] text-sm text-muted-foreground">
							{project.task_id_prefix}
						</p>
					) : null}
				</div>
			</div>

			<Separator className="mb-6" />

			<div className="space-y-4 max-w-md">
				<div className="space-y-1.5">
					<Label htmlFor="project-name">
						{t("settings.general.projectNameLabel")}
					</Label>
					<Input
						id="project-name"
						value={name}
						onChange={(e) => {
							setName(e.target.value);
							setNameError(null);
						}}
						placeholder={t("settings.general.projectNamePlaceholder")}
						disabled={!canEdit}
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

				<div className="space-y-1.5">
					<Label htmlFor="project-prefix">
						{t("settings.general.taskIdPrefixLabel")}{" "}
						<span className="text-muted-foreground font-normal text-xs">
							{t("settings.general.taskIdPrefixHint")}
						</span>
					</Label>
					<Input
						id="project-prefix"
						value={prefix}
						onChange={(e) => {
							setPrefix(
								e.target.value
									.toUpperCase()
									.replace(/[^A-Z0-9]/g, "")
									.slice(0, 10),
							);
							setPrefixError(null);
						}}
						placeholder={t("settings.general.taskIdPrefixPlaceholder")}
						disabled={!canEdit}
						className={`font-[JetBrains_Mono,monospace] uppercase w-32${prefixError ? " border-destructive focus-visible:ring-destructive/30" : ""}`}
						maxLength={10}
					/>
					{prefixError ? (
						<p className="text-xs text-destructive">{prefixError}</p>
					) : null}
				</div>

				<div className="space-y-1.5">
					<Label htmlFor="project-description">
						{t("settings.general.descriptionLabel")}
					</Label>
					<Textarea
						id="project-description"
						value={description}
						onChange={(e) => setDescription(e.target.value)}
						placeholder={t("settings.general.descriptionPlaceholder")}
						rows={3}
						disabled={!canEdit}
						className="resize-none"
					/>
				</div>

				{error ? (
					<p className="text-xs text-destructive bg-destructive/10 rounded-lg px-3 py-2">
						{error}
					</p>
				) : null}

				{canEdit ? (
					<div className="flex items-center gap-2 pt-1">
						<Button
							size="sm"
							disabled={!isDirty || mutation.isPending}
							onClick={() => mutation.mutate()}
							className="gap-1.5"
						>
							{mutation.isPending ? (
								<Spinner aria-hidden="true" className="size-3.5" />
							) : null}
							{t("settings.general.saveChanges")}
						</Button>
					</div>
				) : (
					<p className="text-xs text-muted-foreground">
						{t("settings.general.noPermission")}
					</p>
				)}
			</div>
		</div>
	);
}
