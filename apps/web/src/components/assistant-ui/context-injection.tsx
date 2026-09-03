"use client";

import { useParams } from "@tanstack/react-router";
import {
	FileText,
	Hash,
	Loader2,
	MessageSquare,
	Pin,
	Plus,
	Search,
	Workflow,
} from "lucide-react";
import { type ComponentType, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChipField } from "@/components/projects/interactions/task-detail/property-field/chip-field";
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
	task: "text-emerald-700 dark:text-emerald-400",
	doc: "text-purple-700 dark:text-purple-400",
	conversation: "text-amber-700 dark:text-amber-400",
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

// ── Search popover body ──────────────────────────────────────────────────────

function ContextSearchPanel({
	projectId,
	availableTypes,
	onSelect,
}: {
	projectId: string | undefined;
	availableTypes: readonly ContextItemType[];
	onSelect: (item: ContextItem) => void;
}) {
	const { t } = useTranslation("projects");
	const [type, setType] = useState<ContextItemType>(
		availableTypes[0] ?? "conversation",
	);
	const activeType = availableTypes.includes(type) ? type : availableTypes[0];

	const { search, setSearch, queryTooShort, results, isLoading, pagination } =
		useContextItemSearch(activeType ?? "conversation", projectId, true);

	if (!activeType) return null;

	return (
		<div className="flex flex-col gap-2">
			{availableTypes.length > 1 && (
				<div className="flex items-center gap-0.5 rounded-lg bg-muted/40 p-0.5">
					{availableTypes.map((tabType) => {
						const Icon = CONTEXT_TYPE_ICON[tabType];
						return (
							<button
								key={tabType}
								type="button"
								onClick={() => setType(tabType)}
								className={cn(
									// min-w-0 overrides a flex item's default min-width:auto,
									// which otherwise ignores flex-1's shrinking and grows the
									// button (and the whole row) to fit the untruncated label —
									// exactly what was pushing this row past the popover's box.
									"flex min-w-0 flex-1 items-center justify-center gap-1 rounded-md px-1.5 py-1 text-xs font-medium transition-colors",
									activeType === tabType
										? "bg-background text-foreground shadow-sm"
										: "text-muted-foreground hover:text-foreground",
								)}
							>
								<Icon className="size-3" />
								<span className="truncate">
									{t(CONTEXT_TYPE_LABEL_KEY[tabType])}
								</span>
							</button>
						);
					})}
				</div>
			)}

			<div className="relative">
				<Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground/50" />
				<input
					// biome-ignore lint/a11y/noAutofocus: intentional for popover
					autoFocus
					value={search}
					onChange={(e) => setSearch(e.target.value)}
					placeholder={t(
						"agents.thread.contextInjection.contextSearchPlaceholder",
					)}
					className="w-full rounded-md border border-border/30 bg-muted/20 py-1.5 pr-2 pl-8 text-xs outline-none focus:border-primary/40 focus:ring-2 focus:ring-primary/15"
				/>
			</div>

			<div
				onScroll={createLoadMoreScrollHandler(pagination)}
				className="max-h-96 min-h-10 overflow-y-auto [scrollbar-gutter:stable]"
			>
				{queryTooShort ? (
					<p className="px-2 py-3 text-center text-xs text-muted-foreground/60">
						{t("agents.thread.contextInjection.contextSearchMinChars")}
					</p>
				) : isLoading ? (
					<div className="flex justify-center py-3">
						<Loader2 className="size-4 animate-spin text-muted-foreground/50" />
					</div>
				) : results.length === 0 ? (
					<p className="px-2 py-3 text-center text-xs text-muted-foreground/60">
						{t("agents.thread.contextInjection.noContextResults")}
					</p>
				) : (
					<>
						{results.map((result) => (
							<button
								key={result.id}
								type="button"
								onClick={() =>
									onSelect({
										type: activeType,
										id: result.id,
										title: result.title,
										...(projectId ? { projectId } : {}),
									})
								}
								className="flex w-full flex-col items-start gap-0.5 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-muted/50"
							>
								<span className="w-full truncate text-xs font-medium">
									{result.title}
								</span>
								{result.subtitle && (
									<span className="w-full truncate text-[11px] text-muted-foreground/60">
										{result.subtitle}
									</span>
								)}
							</button>
						))}
						{pagination.isLoadingMore && (
							<div className="flex justify-center py-2">
								<Loader2 className="size-3.5 animate-spin text-muted-foreground/50" />
							</div>
						)}
					</>
				)}
			</div>
		</div>
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
			<ChipField
				chips={items.map((item) => ({
					key: contextItemKey(item.type, item.id),
					label: <RemovableContextItemLabel item={item} />,
				}))}
				onRemoveChip={(key) => {
					const sep = key.indexOf(":");
					remove(key.slice(0, sep) as ContextItemType, key.slice(sep + 1));
				}}
				canEdit
				addLabel={t("agents.thread.contextInjection.attachContext")}
				// ChipField's default popover box (w-52) is sized for its other
				// callers' short tag/user lists — too narrow for this panel's
				// type tabs + search input + result list. Widened to fit, and
				// overflow-hidden clips the box to its own rounded border as a
				// backstop so no child content (e.g. an unexpectedly long
				// result title) can ever visually spill past it again.
				popoverContentClassName="w-96 overflow-hidden p-2.5 rounded-xl border border-border/40 shadow-lg"
			>
				<ContextSearchPanel
					projectId={projectId}
					availableTypes={availableTypes}
					onSelect={add}
				/>
			</ChipField>
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
