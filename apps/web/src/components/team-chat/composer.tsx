import { useQuery } from "@tanstack/react-query";
import { SendHorizontal } from "lucide-react";
import { type KeyboardEvent, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { EntityAvatar } from "@/components/shared/entity-avatar";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import type { ChatBlock } from "@/lib/chat-api";
import {
	type ChatMention,
	MAX_MESSAGE_LENGTH,
	textToBlocks,
} from "@/lib/chat-content";
import { chatErrorKey } from "@/lib/chat-errors";
import { searchMentionableMembers } from "@/lib/mention-api";

const draftKey = (id: string) => `paca:chat-draft:${id}`;

function readDraft(id: string): string {
	try {
		return localStorage.getItem(draftKey(id)) ?? "";
	} catch {
		return "";
	}
}

function writeDraft(id: string, text: string): void {
	try {
		if (text) localStorage.setItem(draftKey(id), text);
		else localStorage.removeItem(draftKey(id));
	} catch {
		/* a draft is a convenience */
	}
}

/**
 * The "@" being typed at the caret: where it starts and what follows it, or
 * null when the caret is not in one. An "@" only counts at the start of the text
 * or after whitespace, so an email address is not a mention.
 */
export function activeMention(
	text: string,
	caret: number,
): { start: number; query: string } | null {
	const before = text.slice(0, caret);
	const m = /(^|\s)@([^\s@]{0,30})$/.exec(before);
	if (!m) return null;
	return { start: before.length - m[2].length - 1, query: m[2] };
}

const MAX_SUGGESTIONS = 6;

/**
 * The message box. Enter sends, Shift+Enter adds a line; the draft is kept per
 * `draftId` (a channel or a thread) so switching away loses nothing. While a
 * send is in flight the text stays, and is cleared only when it succeeded.
 */
export function Composer({
	draftId,
	placeholder,
	disabledReason,
	onSend,
	autoFocus,
	mentionProjectId,
}: {
	/** Offers "@" suggestions from this project's members when set. */
	mentionProjectId?: string;
	draftId: string;
	placeholder: string;
	/** Why posting is not possible; the box is replaced by this text. */
	disabledReason?: string;
	onSend: (content: ChatBlock[]) => Promise<void>;
	autoFocus?: boolean;
}) {
	const { t } = useTranslation("chat");
	const [text, setText] = useState(() => readDraft(draftId));
	const [sending, setSending] = useState(false);
	const [mentions, setMentions] = useState<ChatMention[]>([]);
	const [caret, setCaret] = useState(0);
	const [highlight, setHighlight] = useState(0);
	const [dismissed, setDismissed] = useState<number | null>(null);
	const [fragment, setFragment] = useState("");
	const nextCaret = useRef<number | null>(null);
	const [error, setError] = useState<string | null>(null);
	const ref = useRef<HTMLTextAreaElement>(null);

	// A different channel or thread is a different draft.
	useEffect(() => {
		setText(readDraft(draftId));
		setMentions([]);
		setError(null);
	}, [draftId]);

	const open = mentionProjectId ? activeMention(text, caret) : null;
	const shown = open && dismissed !== open.start ? open : null;
	// Search a moment after typing stops; the pending search is dropped when the
	// text moves on.
	useEffect(() => {
		const id = setTimeout(() => setFragment(shown?.query ?? ""), 150);
		return () => clearTimeout(id);
	}, [shown?.query]);
	const suggestions = useQuery({
		queryKey: ["chat", "mention-search", mentionProjectId, fragment],
		queryFn: () =>
			searchMentionableMembers(mentionProjectId as string, fragment),
		enabled: !!mentionProjectId && !!shown,
		staleTime: 30_000,
	});
	// Only results for what is typed now count: until the search catches up with
	// the text, the list shown is for an older query and must not be picked from.
	const fresh = !!shown && fragment === shown.query && !suggestions.isFetching;
	const options = fresh
		? (suggestions.data ?? []).slice(0, MAX_SUGGESTIONS)
		: [];

	// Put the caret back after the inserted name.
	// biome-ignore lint/correctness/useExhaustiveDependencies: runs after each text change
	useEffect(() => {
		if (nextCaret.current !== null && ref.current) {
			ref.current.setSelectionRange(nextCaret.current, nextCaret.current);
			nextCaret.current = null;
		}
	}, [text]);

	const choose = (m: { id: string; name: string }) => {
		if (!shown) return;
		const insert = `@${m.name} `;
		const next = text.slice(0, shown.start) + insert + text.slice(caret);
		nextCaret.current = shown.start + insert.length;
		setText(next);
		setCaret(shown.start + insert.length);
		writeDraft(draftId, next);
		setMentions((cur) =>
			cur.some((c) => c.id === m.id)
				? cur
				: [...cur, { id: m.id, name: m.name }],
		);
		setHighlight(0);
	};

	useEffect(() => {
		if (autoFocus) ref.current?.focus();
	}, [autoFocus]);

	if (disabledReason) {
		return (
			<p className="border-t px-4 py-3 text-center text-sm text-muted-foreground">
				{disabledReason}
			</p>
		);
	}

	const blocks = textToBlocks(text, mentions);
	const tooLong = text.length > MAX_MESSAGE_LENGTH;
	const canSend = blocks.length > 0 && !tooLong && !sending;

	const submit = async () => {
		if (!canSend) return;
		setSending(true);
		setError(null);
		try {
			await onSend(blocks);
			setText("");
			setMentions([]);
			writeDraft(draftId, "");
		} catch (err) {
			setError(t(`errors.${chatErrorKey(err)}`));
		} finally {
			setSending(false);
			ref.current?.focus();
		}
	};

	const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
		// While the search is catching up, Enter must neither pick from a stale list
		// nor send a half-typed mention.
		if (
			shown &&
			!fresh &&
			e.key === "Enter" &&
			!e.shiftKey &&
			!e.nativeEvent.isComposing
		) {
			e.preventDefault();
			return;
		}
		if (shown && options.length > 0) {
			if (e.key === "ArrowDown" || e.key === "ArrowUp") {
				e.preventDefault();
				const d = e.key === "ArrowDown" ? 1 : -1;
				setHighlight((h) => (h + d + options.length) % options.length);
				return;
			}
			if (
				(e.key === "Enter" || e.key === "Tab") &&
				!e.nativeEvent.isComposing
			) {
				e.preventDefault();
				const pick = options[Math.min(highlight, options.length - 1)];
				if (pick) choose(pick);
				return;
			}
		}
		if (shown && e.key === "Escape") {
			e.preventDefault();
			setDismissed(shown.start);
			return;
		}
		// Not while an IME composition is confirming a character.
		if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
			e.preventDefault();
			void submit();
		}
	};

	return (
		<div className="relative border-t p-3">
			{shown && options.length > 0 ? (
				<ul
					role="listbox"
					aria-label={t("composer.mentionList")}
					className="absolute bottom-full left-3 z-20 mb-1 w-72 overflow-hidden rounded-lg border bg-popover text-popover-foreground shadow-md"
				>
					{options.map((m, i) => (
						<li
							key={m.id}
							role="option"
							aria-selected={i === highlight}
							// mousedown, so the textarea keeps its caret
							onMouseDown={(e) => {
								e.preventDefault();
								choose(m);
							}}
							onMouseEnter={() => setHighlight(i)}
							className={`flex cursor-pointer items-center gap-2 px-3 py-1.5 text-sm ${i === highlight ? "bg-muted" : ""}`}
						>
							<EntityAvatar {...m.avatar} name={m.name} size="xs" />
							<span className="truncate font-medium">{m.name}</span>
							<span className="truncate text-xs text-muted-foreground">
								@{m.username}
							</span>
						</li>
					))}
				</ul>
			) : null}
			<div className="flex items-end gap-2">
				<Textarea
					ref={ref}
					value={text}
					onChange={(e) => {
						setText(e.target.value);
						setCaret(e.target.selectionStart ?? e.target.value.length);
						setHighlight(0);
						writeDraft(draftId, e.target.value);
					}}
					onSelect={(e) => setCaret(e.currentTarget.selectionStart ?? 0)}
					onKeyDown={onKeyDown}
					placeholder={placeholder}
					aria-label={placeholder}
					rows={1}
					className="max-h-40 min-h-9 resize-none"
				/>
				<Button
					type="button"
					size="icon"
					aria-label={t("composer.send")}
					disabled={!canSend}
					onClick={() => void submit()}
				>
					<SendHorizontal />
				</Button>
			</div>
			<p
				className={
					tooLong || error
						? "mt-1 text-xs text-destructive"
						: "mt-1 hidden text-xs text-muted-foreground md:block"
				}
				role={tooLong || error ? "alert" : undefined}
			>
				{tooLong ? t("composer.tooLong") : (error ?? t("composer.hint"))}
			</p>
		</div>
	);
}
