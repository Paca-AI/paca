import { ArrowDown } from "lucide-react";
import { type ReactNode, useLayoutEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";

const NEAR_BOTTOM_PX = 120;

/**
 * The scrolling body of a conversation, oldest first. It stays pinned to the
 * newest entry while the reader is at the bottom, offers a jump button when they
 * are reading history and something new arrives, and keeps its place when older
 * entries are loaded above. `ids` are the entries' stable ids, in order; what
 * each looks like is up to `children`.
 */
export function ConversationScroller({
	ids,
	hasOlder,
	loadingOlder,
	onLoadOlder,
	loadOlderLabel,
	newLabel,
	emptyLabel,
	footer,
	children,
}: {
	ids: string[];
	hasOlder: boolean;
	loadingOlder: boolean;
	onLoadOlder: () => void;
	loadOlderLabel: string;
	/** The jump button, shown while newer entries are out of sight. */
	newLabel: string;
	emptyLabel: string;
	/** After the last entry, such as "Atlas is working". Pinned to like an entry. */
	footer?: ReactNode;
	children: ReactNode;
}) {
	const box = useRef<HTMLDivElement>(null);
	const atBottom = useRef(true);
	const prevCount = useRef(0);
	const prevFirst = useRef<string | undefined>(undefined);
	const prevHeight = useRef(0);
	const [unseen, setUnseen] = useState(false);

	const scrollToEnd = () => {
		const el = box.current;
		if (el) el.scrollTop = el.scrollHeight;
		atBottom.current = true;
		setUnseen(false);
	};

	// biome-ignore lint/correctness/useExhaustiveDependencies: runs when the entries change
	useLayoutEffect(() => {
		const el = box.current;
		if (!el) return;
		const first = ids[0];
		const grewAbove =
			prevFirst.current !== undefined &&
			first !== prevFirst.current &&
			prevCount.current > 0 &&
			ids.length > prevCount.current &&
			ids.includes(prevFirst.current);
		if (grewAbove) {
			// Older entries were added above: keep the reader where they were.
			el.scrollTop += el.scrollHeight - prevHeight.current;
		} else if (ids.length > prevCount.current) {
			if (prevCount.current === 0 || atBottom.current) scrollToEnd();
			else setUnseen(true);
		}
		prevCount.current = ids.length;
		prevFirst.current = first;
		prevHeight.current = el.scrollHeight;
	}, [ids.join("|")]);

	// A footer appearing under the newest entry should not hide it.
	// biome-ignore lint/correctness/useExhaustiveDependencies: follows the footer's presence
	useLayoutEffect(() => {
		if (footer && atBottom.current) scrollToEnd();
	}, [!!footer]);

	return (
		<div className="relative min-h-0 flex-1">
			<div
				ref={box}
				className="absolute inset-0 overflow-y-auto py-2"
				onScroll={(e) => {
					const el = e.currentTarget;
					atBottom.current =
						el.scrollHeight - el.scrollTop - el.clientHeight < NEAR_BOTTOM_PX;
					if (atBottom.current) setUnseen(false);
					prevHeight.current = el.scrollHeight;
				}}
			>
				{hasOlder ? (
					<div className="flex justify-center py-2">
						<Button
							variant="ghost"
							size="sm"
							disabled={loadingOlder}
							onClick={onLoadOlder}
						>
							{loadingOlder ? <Spinner /> : null}
							{loadOlderLabel}
						</Button>
					</div>
				) : null}
				{ids.length === 0 ? (
					<p className="px-4 py-8 text-center text-sm text-muted-foreground">
						{emptyLabel}
					</p>
				) : null}
				{children}
				{footer}
			</div>
			{unseen ? (
				<Button
					size="sm"
					className="absolute bottom-3 left-1/2 -translate-x-1/2 shadow-md"
					onClick={scrollToEnd}
				>
					<ArrowDown />
					{newLabel}
				</Button>
			) : null}
		</div>
	);
}
