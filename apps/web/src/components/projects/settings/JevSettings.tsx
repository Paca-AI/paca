import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	CheckCircle2,
	Eye,
	EyeOff,
	KeyRound,
	Loader2,
	Lock,
	Sparkles,
	Users,
	Wand2,
	XCircle,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import {
	customFieldsQueryOptions,
	projectQueryOptions,
	testProjectJevConfig,
	updateProject,
	updateProjectJevConfig,
} from "@/lib/project-api";

const AUTO_ASSIGN_SCOPE_ALL = "all";
const AUTO_ASSIGN_SCOPE_HUMAN = "human";

interface JevProjectSettings {
	autofillEnabled: boolean;
	autofillExcludedFields: string[];
	autoAssignScope: string;
}

// Mirrors projectdom.ParseJevSettings's graceful-fallback behavior on the Go
// side: any missing or malformed field degrades to the same defaults rather
// than throwing, since Project.Settings is client-writable free-form JSON.
function parseJevSettings(
	settings: Record<string, unknown> | undefined,
): JevProjectSettings {
	const raw = (settings?.jev ?? {}) as Record<string, unknown>;
	return {
		autofillEnabled:
			typeof raw.autofill_enabled === "boolean" ? raw.autofill_enabled : true,
		autofillExcludedFields: Array.isArray(raw.autofill_excluded_fields)
			? raw.autofill_excluded_fields.filter(
					(v): v is string => typeof v === "string",
				)
			: [],
		autoAssignScope:
			raw.auto_assign_scope === AUTO_ASSIGN_SCOPE_HUMAN
				? AUTO_ASSIGN_SCOPE_HUMAN
				: AUTO_ASSIGN_SCOPE_ALL,
	};
}

function sameFieldSet(a: string[], b: string[]): boolean {
	if (a.length !== b.length) return false;
	const sortedA = [...a].sort();
	const sortedB = [...b].sort();
	return sortedA.every((v, i) => v === sortedB[i]);
}

export function JevSettings({
	projectId,
	canEdit,
}: {
	projectId: string;
	canEdit: boolean;
}) {
	const { t } = useTranslation("projects");
	const queryClient = useQueryClient();
	const { data: project } = useQuery(projectQueryOptions(projectId));
	const { data: customFields = [] } = useQuery(
		customFieldsQueryOptions(projectId),
	);

	const saved = parseJevSettings(project?.settings);

	// Provider credentials — separate from the autofill/auto-assign toggles
	// above: these live in dedicated encrypted `projects.jev_*` columns (see
	// UpdateProjectJevConfigRequest), not the free-form `settings` JSONB, and
	// are saved through their own mutation/endpoint. The API key itself is
	// never read back from the server — only jev_configured/jev_base_url/
	// jev_model — so apiKeyInput always starts blank; leaving it blank on
	// save keeps whichever key (if any) is already stored.
	const [apiKeyInput, setApiKeyInput] = useState("");
	const [showApiKey, setShowApiKey] = useState(false);
	const [baseUrlInput, setBaseUrlInput] = useState(project?.jev_base_url ?? "");
	const [modelInput, setModelInput] = useState(project?.jev_model ?? "");
	const [credentialsError, setCredentialsError] = useState<string | null>(null);
	const [credentialsSaved, setCredentialsSaved] = useState(false);

	const credentialsMutation = useMutation({
		mutationFn: (payload: {
			api_key?: string;
			base_url?: string;
			model?: string;
		}) => updateProjectJevConfig(projectId, payload),
		onSuccess: async () => {
			await queryClient.invalidateQueries({
				queryKey: projectQueryOptions(projectId).queryKey,
			});
			setApiKeyInput("");
			setCredentialsError(null);
			setCredentialsSaved(true);
			testMutation.reset();
			setTimeout(() => setCredentialsSaved(false), 2500);
		},
		onError: () => setCredentialsError(t("settings.jev.errors.updateFailed")),
	});

	const credentialsDirty =
		apiKeyInput.trim() !== "" ||
		baseUrlInput.trim() !== (project?.jev_base_url ?? "") ||
		modelInput.trim() !== (project?.jev_model ?? "");

	const handleSaveCredentials = () => {
		const payload: { api_key?: string; base_url?: string; model?: string } = {
			base_url: baseUrlInput.trim(),
			model: modelInput.trim(),
		};
		if (apiKeyInput.trim()) payload.api_key = apiKeyInput.trim();
		credentialsMutation.mutate(payload);
	};

	// Tests whatever credentials are currently *stored* on the server (not
	// unsaved input in the fields above) — mirrors handleSaveCredentials'
	// relationship to credentialsMutation: a separate, explicit action rather
	// than something inferred from field edits.
	const testMutation = useMutation({
		mutationFn: () => testProjectJevConfig(projectId),
	});

	const [autofillEnabled, setAutofillEnabled] = useState(saved.autofillEnabled);
	const [excludedFields, setExcludedFields] = useState<string[]>(
		saved.autofillExcludedFields,
	);
	const [autoAssignScope, setAutoAssignScope] = useState(saved.autoAssignScope);
	const [error, setError] = useState<string | null>(null);
	const [justSaved, setJustSaved] = useState(false);

	// Fixed fields (Importance, Task Type, Story Points, Tags, Epic) always
	// exist on every task, plus whichever custom fields have a fixed,
	// enumerable domain — the same eligibility rule
	// worker.TaskAutofillConsumer applies on the backend (select/
	// multi_select/boolean only; text/number/date/url are excluded there
	// unconditionally since Jev can't produce unconstrained answers).
	const fillableFields = [
		{ key: "importance", label: t("settings.jev.autofill.fieldImportance") },
		{ key: "task_type_id", label: t("settings.jev.autofill.fieldTaskType") },
		{ key: "story_points", label: t("settings.jev.autofill.fieldStoryPoints") },
		{ key: "tags", label: t("settings.jev.autofill.fieldTags") },
		{ key: "parent_task_id", label: t("settings.jev.autofill.fieldEpic") },
		...customFields
			.filter(
				(f) =>
					f.field_type === "select" ||
					f.field_type === "multi_select" ||
					f.field_type === "boolean",
			)
			.map((f) => ({ key: `custom:${f.field_key}`, label: f.display_name })),
	];

	const toggleField = (key: string, enabled: boolean) => {
		setExcludedFields((prev) =>
			enabled
				? prev.filter((k) => k !== key)
				: prev.includes(key)
					? prev
					: [...prev, key],
		);
	};

	const mutation = useMutation({
		mutationFn: () =>
			updateProject(projectId, {
				settings: {
					...(project?.settings ?? {}),
					jev: {
						autofill_enabled: autofillEnabled,
						autofill_excluded_fields: excludedFields,
						auto_assign_scope: autoAssignScope,
					},
				},
			}),
		onSuccess: async (updated) => {
			await queryClient.invalidateQueries({
				queryKey: projectQueryOptions(projectId).queryKey,
			});
			const next = parseJevSettings(updated.settings);
			setAutofillEnabled(next.autofillEnabled);
			setExcludedFields(next.autofillExcludedFields);
			setAutoAssignScope(next.autoAssignScope);
			setError(null);
			setJustSaved(true);
			setTimeout(() => setJustSaved(false), 2500);
		},
		onError: () => setError(t("settings.jev.errors.updateFailed")),
	});

	const isDirty =
		autofillEnabled !== saved.autofillEnabled ||
		!sameFieldSet(excludedFields, saved.autofillExcludedFields) ||
		autoAssignScope !== saved.autoAssignScope;

	return (
		<div className="rounded-xl border border-border/60 bg-card p-6">
			<h3 className="font-[Syne] text-base font-semibold mb-1">
				{t("settings.jev.title")}
			</h3>
			<p className="text-sm text-muted-foreground mb-6">
				{t("settings.jev.description")}
			</p>

			<div className="space-y-6">
				{/* Provider credentials */}
				<div className="rounded-lg border border-border/60 bg-muted/20 p-4">
					<div className="flex items-center gap-3 mb-1">
						<div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 border border-primary/15">
							<KeyRound className="size-4 text-primary" />
						</div>
						<div className="flex-1 min-w-0">
							<Label className="font-medium">
								{t("settings.jev.credentials.title")}
							</Label>
							<div className="flex items-center gap-1.5 mt-0.5">
								<span
									className={`size-1.5 rounded-full ${
										project?.jev_configured
											? "bg-emerald-500"
											: "bg-muted-foreground/40"
									}`}
								/>
								<span className="text-xs text-muted-foreground">
									{project?.jev_configured
										? t("settings.jev.credentials.configuredBadge")
										: t("settings.jev.credentials.notConfiguredBadge")}
								</span>
							</div>
						</div>
					</div>
					<p className="text-xs text-muted-foreground mb-3 leading-relaxed">
						{t("settings.jev.credentials.description")}
					</p>

					<div className="space-y-2.5">
						<div>
							<Label
								htmlFor="jev-api-key"
								className="text-xs font-medium mb-1 flex items-center gap-1"
							>
								<Lock className="size-3" />
								{t("settings.jev.credentials.apiKeyLabel")}
							</Label>
							<div className="relative">
								<Input
									id="jev-api-key"
									type={showApiKey ? "text" : "password"}
									autoComplete="off"
									value={apiKeyInput}
									onChange={(e) => {
										setApiKeyInput(e.target.value);
										testMutation.reset();
									}}
									placeholder={
										project?.jev_configured
											? t(
													"settings.jev.credentials.apiKeyConfiguredPlaceholder",
												)
											: t("settings.jev.credentials.apiKeyPlaceholder")
									}
									disabled={!canEdit}
									className="pr-9"
								/>
								<button
									type="button"
									onClick={() => setShowApiKey((v) => !v)}
									className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
									aria-label={
										showApiKey
											? t("agents.createDialog.hideApiKey")
											: t("agents.createDialog.showApiKey")
									}
								>
									{showApiKey ? (
										<EyeOff className="size-3.5" />
									) : (
										<Eye className="size-3.5" />
									)}
								</button>
							</div>
							<p className="text-xs text-muted-foreground mt-1.5 flex items-center gap-1.5">
								<span className="size-1.5 shrink-0 rounded-full bg-emerald-500 inline-block" />
								{t("agents.createDialog.apiKeyHint")}
							</p>
						</div>

						<div>
							<p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground/80 mb-1.5">
								{t("settings.jev.credentials.advancedLabel")}
							</p>
							<div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5">
								<div>
									<Label
										htmlFor="jev-base-url"
										className="text-xs font-medium mb-1 block"
									>
										{t("settings.jev.credentials.baseUrlLabel")}
									</Label>
									<Input
										id="jev-base-url"
										type="text"
										value={baseUrlInput}
										onChange={(e) => setBaseUrlInput(e.target.value)}
										placeholder={t(
											"settings.jev.credentials.baseUrlPlaceholder",
										)}
										disabled={!canEdit}
									/>
								</div>
								<div>
									<Label
										htmlFor="jev-model"
										className="text-xs font-medium mb-1 block"
									>
										{t("settings.jev.credentials.modelLabel")}
									</Label>
									<Input
										id="jev-model"
										type="text"
										value={modelInput}
										onChange={(e) => setModelInput(e.target.value)}
										placeholder={t("settings.jev.credentials.modelPlaceholder")}
										disabled={!canEdit}
									/>
								</div>
							</div>
						</div>
					</div>

					{credentialsError ? (
						<p className="text-xs text-destructive bg-destructive/10 rounded-lg px-3 py-2 mt-3">
							{credentialsError}
						</p>
					) : null}

					{canEdit ? (
						<div className="flex items-center gap-2 pt-3">
							<Button
								size="sm"
								disabled={!credentialsDirty || credentialsMutation.isPending}
								onClick={handleSaveCredentials}
								className="gap-1.5"
							>
								{credentialsMutation.isPending ? (
									<Loader2 className="size-3.5 animate-spin" />
								) : null}
								{t("settings.jev.credentials.saveButton")}
							</Button>
							{project?.jev_configured ? (
								<Button
									size="sm"
									variant="outline"
									disabled={testMutation.isPending}
									onClick={() => testMutation.mutate()}
									className="gap-1.5"
								>
									{testMutation.isPending ? (
										<Loader2 className="size-3.5 animate-spin" />
									) : null}
									{t("settings.jev.credentials.testButton")}
								</Button>
							) : null}
							{credentialsSaved ? (
								<span className="text-xs text-emerald-600 dark:text-emerald-400 font-medium">
									{t("settings.jev.saved")}
								</span>
							) : null}
							{!credentialsSaved && testMutation.isSuccess ? (
								<span className="text-xs text-emerald-600 dark:text-emerald-400 font-medium flex items-center gap-1">
									<CheckCircle2 className="size-3.5" />
									{t("settings.jev.credentials.testSuccess")}
								</span>
							) : null}
							{!credentialsSaved && testMutation.isError ? (
								<span className="text-xs text-destructive font-medium flex items-center gap-1">
									<XCircle className="size-3.5" />
									{t("settings.jev.credentials.testFailed")}
								</span>
							) : null}
						</div>
					) : null}
				</div>

				<Separator />

				{project?.jev_configured ? (
					<div className="rounded-lg border border-border/60 bg-muted/20 p-4 space-y-4">
						{/* Auto-fill */}
						<div>
							<div className="flex items-center justify-between">
								<div className="flex items-center gap-3">
									<div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 border border-primary/15">
										<Wand2 className="size-4 text-primary" />
									</div>
									<div>
										<Label
											htmlFor="jev-autofill-enabled"
											className="font-medium cursor-pointer"
										>
											{t("settings.jev.autofill.title")}
										</Label>
										<p className="text-xs text-muted-foreground mt-0.5 leading-relaxed">
											{t("settings.jev.autofill.description")}
										</p>
									</div>
								</div>
								<Switch
									id="jev-autofill-enabled"
									checked={autofillEnabled}
									onCheckedChange={setAutofillEnabled}
									disabled={!canEdit}
								/>
							</div>

							{autofillEnabled ? (
								<div className="mt-3 pl-12">
									<p className="text-xs font-medium">
										{t("settings.jev.autofill.excludedFieldsLabel")}
									</p>
									<p className="text-xs text-muted-foreground mt-0.5 mb-2 leading-relaxed">
										{t("settings.jev.autofill.excludedFieldsHint")}
									</p>
									<div className="flex flex-wrap gap-2">
										{fillableFields.map((field) => {
											const enabled = !excludedFields.includes(field.key);
											return (
												<Button
													key={field.key}
													type="button"
													size="sm"
													variant={enabled ? "default" : "outline"}
													disabled={!canEdit}
													onClick={() => toggleField(field.key, !enabled)}
												>
													{field.label}
												</Button>
											);
										})}
									</div>
								</div>
							) : null}
						</div>

						<Separator className="bg-border/50" />

						{/* Auto-assign */}
						<div>
							<div className="flex items-center gap-3">
								<div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 border border-primary/15">
									<Users className="size-4 text-primary" />
								</div>
								<div>
									<Label className="font-medium">
										{t("settings.jev.autoAssign.title")}
									</Label>
									<p className="text-xs text-muted-foreground mt-0.5 leading-relaxed">
										{t("settings.jev.autoAssign.description")}
									</p>
								</div>
							</div>

							<div className="mt-3 pl-12">
								<p className="text-xs font-medium mb-2">
									{t("settings.jev.autoAssign.scopeLabel")}
								</p>
								<div className="flex gap-2">
									<Button
										type="button"
										size="sm"
										variant={
											autoAssignScope === AUTO_ASSIGN_SCOPE_ALL
												? "default"
												: "outline"
										}
										disabled={!canEdit}
										onClick={() => setAutoAssignScope(AUTO_ASSIGN_SCOPE_ALL)}
									>
										{t("settings.jev.autoAssign.scopeAll")}
									</Button>
									<Button
										type="button"
										size="sm"
										variant={
											autoAssignScope === AUTO_ASSIGN_SCOPE_HUMAN
												? "default"
												: "outline"
										}
										disabled={!canEdit}
										onClick={() => setAutoAssignScope(AUTO_ASSIGN_SCOPE_HUMAN)}
									>
										{t("settings.jev.autoAssign.scopeHuman")}
									</Button>
								</div>
								{autoAssignScope === AUTO_ASSIGN_SCOPE_HUMAN ? (
									<p className="text-xs text-muted-foreground mt-2 leading-relaxed">
										{t("settings.jev.autoAssign.scopeHumanHint")}
									</p>
								) : null}
							</div>
						</div>
					</div>
				) : (
					<div className="rounded-xl border border-dashed border-border bg-muted/20 py-16 text-center">
						<div className="mx-auto mb-3 flex size-12 items-center justify-center rounded-full bg-muted text-muted-foreground/60">
							<Sparkles className="size-5" />
						</div>
						<p className="text-sm font-medium">
							{t("settings.jev.notConfigured.title")}
						</p>
						<p className="text-xs text-muted-foreground mt-1 max-w-xs mx-auto leading-relaxed">
							{t("settings.jev.notConfigured.description")}
						</p>
					</div>
				)}

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
							{t("settings.jev.saveChanges")}
						</Button>
						{justSaved ? (
							<span className="text-xs text-emerald-600 dark:text-emerald-400 font-medium">
								{t("settings.jev.saved")}
							</span>
						) : null}
					</div>
				) : (
					<p className="text-xs text-muted-foreground">
						{t("settings.jev.noPermission")}
					</p>
				)}
			</div>
		</div>
	);
}
