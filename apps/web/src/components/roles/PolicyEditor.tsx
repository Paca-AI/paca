import { useQuery } from "@tanstack/react-query";
import {
	BookOpen,
	Braces,
	ChevronDown,
	ExternalLink,
	ListChecks,
	type LucideIcon,
	Search,
	SearchX,
	ShieldAlert,
	X,
} from "lucide-react";
import { useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
	Collapsible,
	CollapsibleContent,
	CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type { PluginKnownPermission } from "@/lib/plugin-api";
import { applyHint, policyHints } from "@/lib/policy";
import {
	attributeSchemaQueryOptions,
	knownActionsQueryOptions,
} from "@/lib/role-api";

import { SimulatePanel } from "./SimulatePanel";
import type { PolicyEditor as PolicyEditorState } from "./use-policy-editor";

const NONE: never[] = [];

export interface PermissionGroupDef {
	domain: string;
	labelKey: string;
	Icon: LucideIcon;
}

interface PolicyEditorProps {
	editor: PolicyEditorState;
	/** The project a project role belongs to; also where the advanced view
	 *  validates, simulates and loads its suggestions. */
	projectId?: string;
	/** The permissions the simple view offers, and how they are grouped. */
	permissions: PluginKnownPermission[];
	groups: readonly PermissionGroupDef[];
	/** Translates the host's own permission and group labels. */
	tScope: (key: string) => string;
	/** "n enabled", already translated. */
	enabledCount: (count: number) => string;
}

/**
 * A role's permissions: a switch per known permission (simple), or the policy
 * document as JSON (advanced). Both edit one policy; see usePolicyEditor.
 */
/** The guide to writing role policies, linked from the Advanced (JSON) view. */
export const IAM_DOCS_URL =
	"https://github.com/Paca-AI/paca/blob/HEAD/docs/guides/iam-authorization.md";

export function PolicyEditor({
	editor,
	projectId,
	permissions,
	groups,
	tScope,
	enabledCount,
}: PolicyEditorProps) {
	const { t } = useTranslation("roles");
	const panelId = useId();
	const [query, setQuery] = useState("");
	// Groups the user folded away; searching shows every group with a match.
	const [folded, setFolded] = useState<ReadonlySet<string>>(new Set());
	const { data: knownActions = NONE } = useQuery(
		knownActionsQueryOptions(projectId),
	);
	const { data: schema = NONE } = useQuery(
		attributeSchemaQueryOptions(projectId),
	);
	const textareaRef = useRef<HTMLTextAreaElement>(null);
	const [caret, setCaret] = useState(0);
	const hintContext =
		editor.mode === "advanced"
			? policyHints(
					editor.json,
					caret,
					knownActions,
					schema.map((d) => d.key),
				)
			: null;
	const pickHint = (value: string) => {
		if (!hintContext) return;
		const next = applyHint(editor.json, caret, hintContext, value);
		editor.setJson(next.text);
		setCaret(next.caret);
		requestAnimationFrame(() => {
			const el = textareaRef.current;
			if (!el) return;
			el.focus();
			el.setSelectionRange(next.caret, next.caret);
		});
	};
	const label = (p: PluginKnownPermission) => p.rawLabel ?? tScope(p.labelKey);
	const description = (p: PluginKnownPermission) =>
		p.rawDescription ?? tScope(p.descriptionKey);

	const needle = query.trim().toLowerCase();
	const matches = (p: PluginKnownPermission) =>
		needle === "" ||
		[label(p), description(p), p.key].some((text) =>
			text.toLowerCase().includes(needle),
		);
	const sections = groups
		.map((group) => {
			const all = permissions.filter((p) => p.domain === group.domain);
			return { group, all, shown: all.filter(matches) };
		})
		.filter((s) => s.all.length > 0);
	const visible = sections.filter((s) => s.shown.length > 0);
	const total = sections.reduce((n, s) => n + s.all.length, 0);
	const enabled = sections.reduce(
		(n, s) => n + s.all.filter((p) => editor.checked[p.key]).length,
		0,
	);
	const toggleFold = (domain: string) =>
		setFolded((prev) => {
			const next = new Set(prev);
			if (!next.delete(domain)) next.add(domain);
			return next;
		});

	return (
		<div className="flex flex-col gap-4">
			<div
				className="grid w-full grid-cols-2 gap-1 rounded-lg bg-muted p-1 sm:inline-grid sm:w-fit"
				role="tablist"
				aria-label={t("editor.modeLabel")}
			>
				{(["simple", "advanced"] as const).map((m) => {
					const Icon = m === "simple" ? ListChecks : Braces;
					const selected = editor.mode === m;
					return (
						<button
							key={m}
							type="button"
							role="tab"
							aria-selected={selected}
							aria-controls={panelId}
							onClick={() => editor.setMode(m)}
							className={`inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium outline-none transition-colors focus-visible:ring-3 focus-visible:ring-ring/50 ${
								selected
									? "bg-background text-foreground shadow-sm ring-1 ring-foreground/10"
									: "text-muted-foreground hover:text-foreground"
							}`}
						>
							<Icon className="size-3.5" aria-hidden="true" />
							{t(m === "simple" ? "editor.simple" : "editor.advanced")}
						</button>
					);
				})}
			</div>

			{editor.blocked ? (
				<div
					role="status"
					className="rounded-lg border border-amber-300/50 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-700/40 dark:bg-amber-900/20 dark:text-amber-300"
				>
					{editor.mode === "advanced" && editor.blocked !== "invalidJson"
						? t("editor.advancedOnly")
						: t("editor.cannotSwitch")}{" "}
					{t(`editor.reasons.${editor.blocked}`)}
				</div>
			) : null}

			<div id={panelId} role="tabpanel" className="flex min-w-0 flex-col gap-4">
				{editor.mode === "simple" ? (
					<>
						{editor.fullAccess ? (
							<div className="flex items-start gap-3 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2.5 dark:border-amber-700/30 dark:bg-amber-900/20">
								<ShieldAlert
									className="mt-0.5 size-4 shrink-0 text-amber-700 dark:text-amber-400"
									aria-hidden="true"
								/>
								<div className="flex min-w-0 flex-col gap-0.5">
									<span className="text-sm font-semibold text-amber-800 dark:text-amber-300">
										{t("editor.fullAccess")}
									</span>
									<p className="text-xs text-amber-800/90 dark:text-amber-300/90">
										{t("editor.fullAccessDescription")}
									</p>
								</div>
							</div>
						) : null}

						{total > 0 ? (
							<div className="flex flex-col gap-2 sm:flex-row sm:items-center">
								<div className="relative flex-1">
									<Search
										className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground"
										aria-hidden="true"
									/>
									<Input
										type="search"
										value={query}
										onChange={(e) => setQuery(e.target.value)}
										placeholder={t("editor.searchPlaceholder")}
										aria-label={t("editor.searchLabel")}
										autoComplete="off"
										className="pl-8"
									/>
								</div>
								{editor.fullAccess ? null : (
									<div className="flex shrink-0 items-center gap-1.5 text-xs">
										<span className="rounded-full bg-primary/10 px-2 py-0.5 font-medium text-primary">
											{enabledCount(enabled)}
										</span>
										<span className="text-muted-foreground">
											{t("editor.ofTotal", { total })}
										</span>
									</div>
								)}
							</div>
						) : null}

						{total === 0 ? (
							<div className="flex flex-col items-center gap-1 rounded-lg border border-dashed py-10 text-center">
								<p className="text-sm font-medium">
									{t("editor.noPermissions")}
								</p>
								<p className="text-xs text-muted-foreground">
									{t("editor.noPermissionsHint")}
								</p>
							</div>
						) : visible.length === 0 ? (
							<div className="flex flex-col items-center gap-2 rounded-lg border border-dashed py-10 text-center">
								<SearchX
									className="size-5 text-muted-foreground"
									aria-hidden="true"
								/>
								<p className="text-sm font-medium">
									{t("editor.noMatches", { query: query.trim() })}
								</p>
								<Button
									type="button"
									variant="outline"
									size="sm"
									onClick={() => setQuery("")}
								>
									<X className="size-3.5" aria-hidden="true" />
									{t("editor.clearSearch")}
								</Button>
							</div>
						) : (
							<div className="flex flex-col gap-3">
								{visible.map(({ group, all, shown }) => {
									const { Icon } = group;
									const groupName = tScope(group.labelKey);
									const on = all.filter((p) => editor.checked[p.key]).length;
									const allOn = on === all.length;
									const open = needle !== "" || !folded.has(group.domain);
									return (
										<Collapsible
											key={group.domain}
											open={open}
											onOpenChange={() => {
												if (needle === "") toggleFold(group.domain);
											}}
											className="overflow-hidden rounded-lg border bg-card"
										>
											<div className="flex items-center gap-2 bg-muted/40 px-3 py-2">
												<CollapsibleTrigger className="flex min-w-0 flex-1 items-center gap-2 rounded-md py-0.5 text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
													<ChevronDown
														className={`size-4 shrink-0 text-muted-foreground transition-transform ${open ? "" : "-rotate-90"}`}
														aria-hidden="true"
													/>
													<Icon
														className="size-4 shrink-0 text-muted-foreground"
														aria-hidden="true"
													/>
													<span className="truncate text-sm font-semibold">
														{groupName}
													</span>
												</CollapsibleTrigger>
												<span
													className={`shrink-0 rounded-full px-2 py-0.5 text-xs tabular-nums ${on > 0 ? "bg-primary/10 font-medium text-primary" : "bg-muted text-muted-foreground"}`}
												>
													{t("editor.groupCount", { on, total: all.length })}
												</span>
												<Button
													type="button"
													variant="ghost"
													size="sm"
													className="h-7 shrink-0 px-2 text-xs"
													aria-label={t(
														allOn ? "editor.clearGroup" : "editor.selectGroup",
														{ group: groupName },
													)}
													onClick={() =>
														editor.setMany(
															all.map((p) => p.key),
															!allOn,
														)
													}
												>
													{t(allOn ? "editor.none" : "editor.all")}
												</Button>
											</div>
											<CollapsibleContent>
												<ul className="divide-y border-t">
													{shown.map((permission) => (
														<li
															key={permission.key}
															className="flex items-center justify-between gap-4 px-3 py-2.5"
														>
															<div className="flex min-w-0 flex-col gap-0.5">
																<span className="text-sm font-medium">
																	{label(permission)}
																</span>
																<span className="text-xs text-muted-foreground">
																	{description(permission)}
																</span>
															</div>
															<Switch
																aria-label={label(permission)}
																checked={!!editor.checked[permission.key]}
																onCheckedChange={(next) =>
																	editor.toggle(permission.key, next)
																}
															/>
														</li>
													))}
												</ul>
											</CollapsibleContent>
										</Collapsible>
									);
								})}
							</div>
						)}
					</>
				) : (
					<div className="flex flex-col gap-3">
						<div className="flex flex-col gap-1.5">
							<a
								href={IAM_DOCS_URL}
								target="_blank"
								rel="noopener noreferrer"
								className="inline-flex w-fit items-center gap-1 text-xs font-medium text-primary hover:underline"
							>
								<BookOpen className="size-3.5" aria-hidden="true" />
								{t("editor.docsLink")}
								<ExternalLink className="size-3" aria-hidden="true" />
							</a>
							<Textarea
								ref={textareaRef}
								aria-label={t("editor.jsonLabel")}
								aria-invalid={!!editor.parseError || editor.issues.length > 0}
								spellCheck={false}
								wrap="off"
								value={editor.json}
								onChange={(e) => {
									editor.setJson(e.target.value);
									setCaret(e.target.selectionStart);
								}}
								onSelect={(e) => setCaret(e.currentTarget.selectionStart)}
								className="field-sizing-fixed h-72 max-h-[50svh] resize-y overflow-auto whitespace-pre bg-muted/20 font-mono text-xs leading-5"
							/>
							{hintContext ? (
								<div className="flex flex-wrap items-center gap-1.5">
									<span className="text-xs text-muted-foreground">
										{t("editor.suggestions")}
									</span>
									<div
										role="listbox"
										aria-label={t("editor.suggestions")}
										className="flex flex-wrap gap-1"
									>
										{hintContext.hints.map((h) => (
											<button
												key={`${h.kind}:${h.value}`}
												type="button"
												role="option"
												aria-selected={false}
												// Keep the caret in the textarea while picking.
												onMouseDown={(e) => e.preventDefault()}
												onClick={() => pickHint(h.value)}
												className="rounded-md border bg-muted/40 px-1.5 py-0.5 font-mono text-xs outline-none hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50"
												title={t(
													h.kind === "action"
														? "editor.suggestionAction"
														: "editor.suggestionAttribute",
												)}
											>
												{h.value}
											</button>
										))}
									</div>
								</div>
							) : null}
							{editor.parseError ? (
								<p
									role="alert"
									className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive"
								>
									{t("editor.invalidJson", { message: editor.parseError })}
								</p>
							) : editor.issues.length > 0 ? (
								<ul
									role="alert"
									className="flex flex-col gap-1 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive"
								>
									{editor.issues.map((issue) => (
										<li key={`${issue.path}:${issue.message}`}>
											<code className="font-mono">{issue.path}</code>{" "}
											{issue.message}
										</li>
									))}
								</ul>
							) : (
								<p className="text-xs text-muted-foreground">
									{editor.validating
										? t("editor.validating")
										: t("editor.jsonHint")}
								</p>
							)}
						</div>
						<SimulatePanel
							json={editor.json}
							projectId={projectId}
							actions={knownActions}
							schema={schema}
						/>
					</div>
				)}
			</div>
		</div>
	);
}
