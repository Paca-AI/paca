import { ArrowDown } from "lucide-react";
import { type FC, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	useMessageScroller,
	useMessageScrollerScrollable,
} from "@/components/ui/message-scroller";
import { Spinner } from "@/components/ui/spinner";

/**
 * Loads older events as the reader scrolls near the top of what is loaded.
 * Keeping the reader's place while older events are prepended is the message
 * scroller's job (`preserveScrollOnPrepend`), so this only has to notice when
 * the sentinel above the oldest loaded message comes into view.
 *
 * Must render inside the message scroller's viewport.
 */
export const LoadOlderEvents: FC<{
	hasOlder: boolean;
	isLoadingOlder: boolean;
	loadOlder: () => void;
}> = ({ hasOlder, isLoadingOlder, loadOlder }) => {
	const { t } = useTranslation("projects");
	const sentinelRef = useRef<HTMLDivElement | null>(null);

	// Tracks whether the sentinel is on screen — rootMargin fires it a little
	// before the reader hits the literal top, so the next page is already
	// loading by the time they get there instead of hitting a dead stop.
	const [sentinelVisible, setSentinelVisible] = useState(false);
	useEffect(() => {
		const sentinel = sentinelRef.current;
		if (!sentinel || !hasOlder) return;
		const viewport = sentinel.closest<HTMLElement>(
			'[data-slot="message-scroller-viewport"]',
		);
		const observer = new IntersectionObserver(
			([entry]) => setSentinelVisible(entry.isIntersecting),
			{ root: viewport, rootMargin: "150px 0px 0px 0px" },
		);
		observer.observe(sentinel);
		return () => observer.disconnect();
	}, [hasOlder]);

	// Fires on becoming visible, and again each time a fetch completes while
	// still visible — covers both "scrolled to the top" and "stayed at the
	// top through several consecutive pages".
	// biome-ignore lint/correctness/useExhaustiveDependencies: loadOlder is a new closure every render — depending on it would refire this effect on every render instead of only on the visibility/hasOlder/isLoadingOlder transitions it actually cares about.
	useEffect(() => {
		if (sentinelVisible && hasOlder && !isLoadingOlder) loadOlder();
	}, [sentinelVisible, hasOlder, isLoadingOlder]);

	if (!hasOlder) return null;

	return (
		<div ref={sentinelRef} className="flex justify-center py-2">
			{isLoadingOlder && (
				<div className="flex items-center gap-1.5 text-muted-foreground text-xs">
					<Spinner aria-hidden="true" className="size-3" />
					{t("agents.conversationView.loadingOlder")}
				</div>
			)}
		</div>
	);
};

/**
 * Tracks whether the reader is at the newest event, and offers a way back when
 * events have arrived while they were reading history.
 *
 * Must render inside the message scroller.
 */
export const TailFollowIndicator: FC<{
	newBelow: number;
	setFollowing: (following: boolean) => void;
	jumpToLatest: () => void;
	/** Increments on every jumpToLatest(). */
	jumpCount: number;
}> = ({ newBelow, setFollowing, jumpToLatest, jumpCount }) => {
	const { t } = useTranslation("projects");
	// `end` is "there is more below", i.e. NOT at the bottom.
	const isAtBottom = !useMessageScrollerScrollable().end;
	const { scrollToEnd } = useMessageScroller();

	// Mirror the viewport into `following` only when the viewport changes.
	// DO NOT add `following` to the deps: the effect would then re-run when
	// jumpToLatest() flips it to true while the viewport is not yet at the
	// bottom, and set it straight back to false, withholding the sender's
	// message and the reply.
	useEffect(() => {
		setFollowing(isAtBottom);
	}, [isAtBottom, setFollowing]);

	// jumpToLatest() (the pill, or sending a message) also brings the viewport
	// to the tail, so following and the viewport don't diverge. The first run
	// is the mount, not a jump. The ref is seeded from the current count, so a
	// remount of this component never replays an old jump.
	const lastJumpRef = useRef(jumpCount);
	useEffect(() => {
		if (lastJumpRef.current === jumpCount) return;
		lastJumpRef.current = jumpCount;
		scrollToEnd({ behavior: "auto" });
	}, [jumpCount, scrollToEnd]);

	const show = newBelow > 0 && !isAtBottom;
	if (!show) return null;

	return (
		<div
			className="sticky bottom-2 z-10 flex justify-center"
			aria-live="polite"
		>
			<Button
				size="sm"
				variant="secondary"
				className="gap-1.5 rounded-full text-xs shadow-md"
				onClick={jumpToLatest}
			>
				<ArrowDown className="size-3" />
				{t("agents.conversationView.newBelow", { count: newBelow })}
			</Button>
		</div>
	);
};
