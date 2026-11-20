import type { ReactNode } from "react";
import type { ChatBlock, ChatInline } from "@/lib/chat-api";
import { safeHref } from "@/lib/chat-content";

// A light renderer for message content: paragraphs, lists, quotes and code,
// with bold, italic, strike and inline code. It reads BlockNote-style blocks
// without mounting an editor per message, which would be too heavy for a long
// channel. A node it does not know renders as its text.

function renderInline(
	nodes: ChatInline[] | undefined,
	me?: string,
): ReactNode[] {
	return (nodes ?? []).map((n, i) => {
		const key = `${i}-${n.type}`;
		if (n.type === "teamMention") {
			// A mention of the reader stands out; the name is plain text, never markup.
			const mine = !!me && n.props?.id === me;
			return (
				<span
					key={key}
					data-mention={mine ? "me" : "other"}
					className={
						mine
							? "rounded bg-primary/20 px-1 font-medium text-primary"
							: "rounded bg-muted px-1 font-medium"
					}
				>
					@{String(n.props?.name ?? "")}
				</span>
			);
		}
		if (n.type === "link") {
			const href = safeHref(n.href);
			const inner = renderInline(n.content, me);
			return href ? (
				<a
					key={key}
					href={href}
					target="_blank"
					rel="noopener noreferrer"
					className="text-primary underline underline-offset-2"
				>
					{inner}
				</a>
			) : (
				<span key={key}>{inner}</span>
			);
		}
		let el: ReactNode = n.text ?? renderInline(n.content, me);
		const s = n.styles ?? {};
		if (s.code) {
			el = (
				<code className="rounded bg-muted px-1 py-0.5 font-mono text-[0.85em]">
					{el}
				</code>
			);
		}
		if (s.bold) el = <strong>{el}</strong>;
		if (s.italic) el = <em>{el}</em>;
		if (s.strike) el = <s>{el}</s>;
		return <span key={key}>{el}</span>;
	});
}

function Block({ block, me }: { block: ChatBlock; me?: string }) {
	const kids = block.children?.length ? (
		<div className="ml-4">
			{block.children.map((c, i) => (
				// biome-ignore lint/suspicious/noArrayIndexKey: blocks have no ids
				<Block key={i} block={c} me={me} />
			))}
		</div>
	) : null;
	const inline = renderInline(block.content, me);
	switch (block.type) {
		case "bulletListItem":
			return (
				<>
					<div className="flex gap-2">
						<span aria-hidden>•</span>
						<span>{inline}</span>
					</div>
					{kids}
				</>
			);
		case "numberedListItem":
			return (
				<>
					<div className="flex gap-2">
						<span aria-hidden>–</span>
						<span>{inline}</span>
					</div>
					{kids}
				</>
			);
		case "quote":
			return (
				<blockquote className="border-l-2 border-border pl-3 text-muted-foreground">
					{inline}
				</blockquote>
			);
		case "codeBlock":
			return (
				<pre className="overflow-x-auto rounded-md bg-muted p-2 font-mono text-xs">
					{inline}
				</pre>
			);
		default:
			return (
				<>
					<p className="whitespace-pre-wrap break-words">{inline}</p>
					{kids}
				</>
			);
	}
}

export function MessageContent({
	blocks,
	currentUserId,
}: {
	blocks: ChatBlock[];
	/** The reader: mentions of them are highlighted. */
	currentUserId?: string;
}) {
	return (
		<div className="space-y-1 text-sm leading-relaxed">
			{blocks.map((b, i) => (
				// biome-ignore lint/suspicious/noArrayIndexKey: blocks have no ids
				<Block key={i} block={b} me={currentUserId} />
			))}
		</div>
	);
}
