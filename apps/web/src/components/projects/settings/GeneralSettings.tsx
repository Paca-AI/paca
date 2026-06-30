import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Globe, Loader2, Lock } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
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
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const { data: project } = useQuery(projectQueryOptions(projectId));

	const [name, setName] = useState(project?.name ?? "");
	const [description, setDescription] = useState(project?.description ?? "");
	const [prefix, setPrefix] = useState(project?.task_id_prefix ?? "");
	const [isPublic, setIsPublic] = useState(project?.is_public ?? false);
	const [nameError, setNameError] = useState<string | null>(null);
	const [prefixError, setPrefixError] = useState<string | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [saved, setSaved] = useState(false);

	const mutation = useMutation({
		mutationFn: () =>
			updateProject(projectId, {
				name: name.trim(),
				description: description.trim(),
				task_id_prefix: prefix.trim() || undefined,
				is_public: isPublic,
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
			setIsPublic(updated.is_public);
			setError(null);
			setNameError(null);
			setPrefixError(null);
			setSaved(true);
			setTimeout(() => setSaved(false), 2500);
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			if (code === ApiErrorCode.ProjectNameTaken) {
				setNameError(t("project.settings.general.errNameTaken"));
				return;
			}
			if (code === ApiErrorCode.ProjectNameInvalid) {
				setNameError(t("project.settings.general.errNameInvalid"));
				return;
			}
			if (code === ApiErrorCode.ProjectPrefixInvalid) {
				setPrefixError(t("project.settings.general.errPrefixInvalid"));
				return;
			}
			setError(t("project.settings.general.errUpdateFailed"));
		},
	});

	const isDirty =
		name.trim() !== (project?.name ?? "") ||
		description.trim() !== (project?.description ?? "") ||
		prefix.trim() !== (project?.task_id_prefix ?? "") ||
		isPublic !== (project?.is_public ?? false);

	return (
		<div className="rounded-xl border border-border/60 bg-card p-6">
			<h3 className="font-[Syne] text-base font-semibold mb-4">
				{t("project.settings.general.title")}
			</h3>
			<div className="space-y-4 max-w-md">
				<div className="space-y-1.5">
					<Label htmlFor="project-name">
						{t("project.settings.general.name")}
					</Label>
					<Input
						id="project-name"
						value={name}
						onChange={(e) => {
							setName(e.target.value);
							setNameError(null);
						}}
						placeholder={t("project.settings.general.namePlaceholder")}
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
						{t("project.settings.general.prefix")}{" "}
						<span className="text-muted-foreground font-normal text-xs">
							{t("project.settings.general.prefixHint")}
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
						placeholder={t("project.settings.general.prefixPlaceholder")}
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
						{t("project.settings.general.description")}
					</Label>
					<Textarea
						id="project-description"
						value={description}
						onChange={(e) => setDescription(e.target.value)}
						placeholder={t("project.settings.general.descriptionPlaceholder")}
						rows={3}
						disabled={!canEdit}
						className="resize-none"
					/>
				</div>

				<div className="flex items-center justify-between rounded-lg border border-border/60 bg-muted/20 px-4 py-3">
					<div className="flex items-start gap-3">
						<div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 border border-primary/15">
							{isPublic ? (
								<Globe className="size-4 text-primary" />
							) : (
								<Lock className="size-4 text-primary" />
							)}
						</div>
						<div>
							<Label htmlFor="is-public" className="font-medium cursor-pointer">
								{t("project.settings.general.publicProject")}
							</Label>
							<p className="text-xs text-muted-foreground mt-0.5 leading-relaxed">
								{isPublic
									? t("project.settings.general.publicDescOn")
									: t("project.settings.general.publicDescOff")}
							</p>
						</div>
					</div>
					<Switch
						id="is-public"
						checked={isPublic}
						onCheckedChange={setIsPublic}
						disabled={!canEdit}
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
								<Loader2 className="size-3.5 animate-spin" />
							) : null}
							{t("project.settings.general.saveChanges")}
						</Button>
						{saved ? (
							<span className="text-xs text-emerald-600 dark:text-emerald-400 font-medium">
								{t("project.settings.general.saved")}
							</span>
						) : null}
					</div>
				) : (
					<p className="text-xs text-muted-foreground">
						{t("project.settings.general.noPermission")}
					</p>
				)}
			</div>
		</div>
	);
}
