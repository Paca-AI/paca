"use client";

import { useParams } from "@tanstack/react-router";
import {
	Check,
	FileText,
	Hash,
	MessageSquare,
	Pin,
	Plus,
	Search,
	Workflow,
	X,
} from "lucide-react";
import { type ComponentType, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import {
	InputGroup,
	InputGroupAddon,
	InputGroupInput,
} from "@/components/ui/input-group";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useContextItemSearch } from "@/hooks/use-context-item-search";
import { useContextInjectionStore } from "@/lib/context-injection-store";
import type { ContextItem, ContextItemType } from "@/lib/context-items";
import { createLoadMoreScrollHandler } from "@/lib/scroll-pagination";
import { useCurrentContextStore } from "@/lib/shortcuts/current-context-store";
import { cn } from "@/lib/utils";

// Per-type icon/color, scoped locally to this file only. task/doc match the
// palette blocknote-inline-contents.tsx already uses for its rich-text
// mention chips (emerald/Hash, purple/FileText) for visual continuity;
// conversation (amber/MessageSquare) and automation (sky/Workflow) are new.
// annotation is rose/Pin here specifically — NOT the amber/MessageSquare its
// own BlockNote mention chip uses, since that pair is already taken by
// conversation in this file and would make the two tabs/chips
// indistinguishable. Intentionally not shared with/imported from
// blocknote-inline-contents.tsx — that file is a separate system (rich-text
// body mentions), out of scope here.
const CONTEXT_TYPE_ICON: Record<
	ContextItemType,
	ComponentType<{ className?: string }>
> = {
	task: Hash,
	doc: FileText,
	conversation: MessageSquare,
	automation: Workflow,
	annotation: Pin,
};

const CONTEXT_TYPE_TEXT_CLASS: Record<ContextItemType, string> = {
	task: "text-success",
	doc: "text-purple-700 dark:text-purple-400",
	conversation: "text-warning",
	automation: "text-sky-700 dark:text-sky-400",
	annotation: "text-rose-700 dark:text-rose-400",
};

// No `Record<ContextItemType, string>` annotation here on purpose — it would
// widen every value to `string`, which the project's typed-i18next `t()`
// rejects (it only accepts literal known keys). `as const` keeps each value
// a literal type instead.
const CONTEXT_TYPE_LABEL_KEY = {
	task: "agents.thread.contextInjection.contextTypeTask",
	doc: "agents.thread.contextInjection.contextTypeDoc",
	conversation: "agents.thread.contextInjection.contextTypeConversation",
	automation: "agents.thread.contextInjection.contextTypeAutomation",
	annotation: "agents.thread.contextInjection.contextTypeAnnotation",
} as const satisfies Record<ContextItemType, string>;

function contextItemKey(type: ContextItemType, id: string): string {
	return `${type}:${id}`;
}

function ContextItemLabel({ item }: { item: ContextItem }) {
	const Icon = CONTEXT_TYPE_ICON[item.type];
	return (
		<span
			className={cn(
				"inline-flex min-w-0 items-center gap-1",
				CONTEXT_TYPE_TEXT_CLASS[item.type],
			)}
		>
			<Icon className="size-3 shrink-0" />
			<span className="max-w-32 truncate text-foreground/80">{item.title}</span>
		</span>
	);
}

/** Same label, plus a hover tooltip explaining the adjacent X removes it —
 *  only used for the editable chip row (ChipField's remove button has no
 *  accessible name of its own and can't be customized without modifying
 *  that shared component, so this tooltip lives on the label content
 *  ChipField renders right next to it instead). Not used by
 *  ContextItemReadOnlyRow, which has no remove action to describe. */
function RemovableContextItemLabel({ item }: { item: ContextItem }) {
	const { t } = useTranslation("projects");
	return (
		<span
			title={t("agents.thread.contextInjection.removeContextItem", {
				title: item.title,
			})}
		>
			<ContextItemLabel item={item} />
		</span>
	);
}

/**
 * Read-only badge row for a historical message's attached context — see
 * thread.tsx's UserMessage, which reads metadata.custom.contextItems off the
 * message and renders this. No remove buttons, no "+" trigger.
 */
export function ContextItemReadOnlyRow({ items }: { items: ContextItem[] }) {
	if (items.length === 0) return null;
	return (
		<div className="col-span-full col-start-1 row-start-1 flex w-full flex-wrap items-center justify-end gap-1.5">
			{items.map((item) => (
				<span
					key={contextItemKey(item.type, item.id)}
					className="inline-flex items-center gap-1 rounded-md border border-border/20 bg-muted/50 px-2 py-0.5 text-xs font-medium"
				>
					<ContextItemLabel item={item} />
				</span>
			))}
		</div>
	);
}

// ── Picker dialog ────────────────────────────────────────────────────────────

/**
 * Dialog for attaching context: pick a type, search, and click results to
 * attach or detach them. It stays open so several items can be attached in one
 * go; "Done" (or Escape) closes it. z-80 keeps it above the floating chat
 * panel (z-70), which this is opened from.
 */
function ContextPickerDialog({
	open,
	onOpenChange,
	projectId,
	availableTypes,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	projectId: string | undefined;
	availableTypes: readonly ContextItemType[];
}) {
	const { t } = useTranslation("projects");
	const items = useContextInjectionStore((s) => s.items);
	const add = useContextInjectionStore((s) => s.add);
	const remove = useContextInjectionStore((s) => s.remove);
	const [type, setType] = useState<ContextItemType>(
		availableTypes[0] ?? "conversation",
	);
	const activeType = availableTypes.includes(type) ? type : availableTypes[0];

	const { search, setSearch, queryTooShort, results, isLoading, pagination } =
		useContextItemSearch(activeType ?? "conversation", projectId, open);

	if (!activeType) return null;

	const isAttached = (id: string) =>
		items.some((i) => i.type === activeType && i.id === id);

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="z-80 sm:max-w-lg" overlayClassName="z-80">
				<DialogHeader>
					<DialogTitle>
						{t("agents.thread.contextInjection.attachContext")}
					</DialogTitle>
					<DialogDescription>
						{t("agents.thread.contextInjection.dialogDescription")}
					</DialogDescription>
				</DialogHeader>

				{availableTypes.length > 1 && (
					<Tabs
						value={activeType}
						onValueChange={(v) => setType(v as ContextItemType)}
					>
						<TabsList className="w-full">
							{availableTypes.map((tabType) => {
								const Icon = CONTEXT_TYPE_ICON[tabType];
								return (
									<TabsTrigger key={tabType} value={tabType}>
										<Icon />
										{t(CONTEXT_TYPE_LABEL_KEY[tabType])}
									</TabsTrigger>
								);
							})}
						</TabsList>
					</Tabs>
				)}

				<InputGroup>
					<InputGroupAddon>
						<Search />
					</InputGroupAddon>
					<InputGroupInput
						// biome-ignore lint/a11y/noAutofocus: the search box is the point of opening the dialog
						autoFocus
						value={search}
						onChange={(e) => setSearch(e.target.value)}
						placeholder={t(
							"agents.thread.contextInjection.contextSearchPlaceholder",
						)}
						aria-label={t(
							"agents.thread.contextInjection.contextSearchPlaceholder",
						)}
					/>
				</InputGroup>

				<div
					onScroll={createLoadMoreScrollHandler(pagination)}
					className="-mx-1 max-h-[50vh] min-h-32 overflow-y-auto px-1 [scrollbar-gutter:stable]"
				>
					{queryTooShort ? (
						<p className="px-2 py-6 text-center text-sm text-muted-foreground">
							{t("agents.thread.contextInjection.contextSearchMinChars")}
						</p>
					) : isLoading ? (
						<div className="flex justify-center py-6">
							<Spinner
								aria-hidden="true"
								className="text-muted-foreground/60"
							/>
						</div>
					) : results.length === 0 ? (
						<p className="px-2 py-6 text-center text-sm text-muted-foreground">
							{t("agents.thread.contextInjection.noContextResults")}
						</p>
					) : (
						<>
							{results.map((result) => {
								const attached = isAttached(result.id);
								return (
									<button
										key={result.id}
										type="button"
										aria-pressed={attached}
										onClick={() =>
											attached
												? remove(activeType, result.id)
												: add({
														type: activeType,
														id: result.id,
														title: result.title,
														...(projectId ? { projectId } : {}),
													})
										}
										className={cn(
											"flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left transition-colors hover:bg-muted/60 focus-visible:bg-muted/60 focus-visible:outline-none",
											attached && "bg-primary/5",
										)}
									>
										<span className="flex min-w-0 flex-1 flex-col gap-0.5">
											<span className="truncate text-sm font-medium">
												{result.title}
											</span>
											{result.subtitle && (
												<span className="truncate text-xs text-muted-foreground">
													{result.subtitle}
												</span>
											)}
										</span>
										{attached && (
											<Check
												className="size-4 shrink-0 text-primary"
												aria-hidden="true"
											/>
										)}
									</button>
								);
							})}
							{pagination.isLoadingMore && (
								<div className="flex justify-center py-2">
									<Spinner
										aria-hidden="true"
										className="size-3.5 text-muted-foreground/60"
									/>
								</div>
							)}
						</>
					)}
				</div>

				<DialogFooter className="items-center sm:justify-between">
					<span className="text-xs text-muted-foreground">
						{t("agents.thread.contextInjection.attachedCount", {
							count: items.length,
						})}
					</span>
					<Button type="button" onClick={() => onOpenChange(false)}>
						{t("agents.thread.contextInjection.done")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

// ── Composer row ──────────────────────────────────────────────────────────────

/**
 * The interactive badge row + "+" search popover + one-click quick-add,
 * mounted inside Composer (thread.tsx) above the message textarea. Reads its
 * project scope from the current route (rather than a prop threaded through
 * Thread/Composer, which take none) — exactly one Composer is ever mounted
 * at a time, and it always lives under whichever route (project-scoped or
 * global) is currently active, so useParams here reflects the right scope
 * without any plumbing changes to Thread itself.
 */
export function ContextInjectionRow() {
	const { t } = useTranslation("projects");
	const { projectId } = useParams({ strict: false });
	const items = useContextInjectionStore((s) => s.items);
	const remove = useContextInjectionStore((s) => s.remove);
	const add = useContextInjectionStore((s) => s.add);
	const active = useCurrentContextStore((s) => s.active);
	const [pickerOpen, setPickerOpen] = useState(false);

	// useContextInjectionStore is a single global store (see its own doc
	// comment for why that's safe — only one Composer is ever mounted at a
	// time), but that means it never unmounts along with any one composer
	// on its own. Without this, staging a badge in one project's chat, then
	// closing the panel without sending and opening a different project's
	// (or the global) chat instead, would carry that stale badge along into
	// an unrelated conversation. Clearing on unmount handles the floating
	// widgets' open/close toggle (their panel contents genuinely unmount);
	// clearing on a projectId change additionally handles a route
	// transition that preserves this component's instance instead of
	// remounting it.
	// biome-ignore lint/correctness/useExhaustiveDependencies: projectId is a deliberate trigger-only dep, unread in the body — clear on unmount *and* on change.
	useEffect(() => {
		return () => {
			useContextInjectionStore.getState().clear();
		};
	}, [projectId]);

	// Task/Doc/Automation search is project-scoped only — hide those tabs
	// entirely on the global (no-project) chat surfaces, where only
	// Conversation search applies. "annotation" is deliberately never in
	// this list — there's no searchable picker tab for comments, only
	// paste-to-attach (see thread.tsx's onPaste) — but it stays a valid
	// ContextItemType (CONTEXT_TYPE_ICON/TEXT_CLASS/LABEL_KEY above still
	// need an "annotation" entry each) since a pasted-and-attached comment
	// still renders as a chip here like any other type.
	const availableTypes: readonly ContextItemType[] = projectId
		? ["task", "doc", "conversation", "automation"]
		: ["conversation"];

	const showQuickAdd =
		!!active &&
		!items.some((i) => i.type === active.type && i.id === active.id);

	return (
		<div className="flex flex-wrap items-center gap-1.5 empty:hidden">
			{items.map((item) => (
				<span
					key={contextItemKey(item.type, item.id)}
					className="inline-flex items-center gap-1 rounded-md border border-border/20 bg-muted/50 px-2 py-0.5 text-xs font-medium text-foreground/80"
				>
					<RemovableContextItemLabel item={item} />
					<button
						type="button"
						onClick={() => remove(item.type, item.id)}
						aria-label={t("agents.thread.contextInjection.removeContextItem", {
							title: item.title,
						})}
						className="text-muted-foreground/60 transition-colors hover:text-destructive"
					>
						<X className="size-2.5" />
					</button>
				</span>
			))}
			<button
				type="button"
				onClick={() => setPickerOpen(true)}
				className="inline-flex items-center gap-1 rounded-md border border-dashed border-border/30 px-2 py-0.5 text-xs text-muted-foreground/60 transition-all duration-150 hover:border-border/60 hover:text-muted-foreground"
			>
				<Plus className="size-2.5" />
				{t("agents.thread.contextInjection.attachContext")}
			</button>
			<ContextPickerDialog
				open={pickerOpen}
				onOpenChange={setPickerOpen}
				projectId={projectId}
				availableTypes={availableTypes}
			/>
			{showQuickAdd && active && (
				<button
					type="button"
					onClick={() => add(active)}
					title={t("agents.thread.contextInjection.addCurrent", {
						title: active.title,
					})}
					className="inline-flex max-w-48 items-center gap-1 rounded-md border border-dashed border-border/40 px-2 py-0.5 text-xs text-muted-foreground transition-colors duration-150 hover:border-primary/50 hover:text-primary"
				>
					<Plus className="size-2.5 shrink-0" />
					<span className="truncate">
						{t("agents.thread.contextInjection.addCurrent", {
							title: active.title,
						})}
					</span>
				</button>
			)}
		</div>
	);
}
