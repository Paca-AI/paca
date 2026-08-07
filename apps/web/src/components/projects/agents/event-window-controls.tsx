import { useThreadViewport } from "@assistant-ui/react";
import { ArrowDown, Loader2 } from "lucide-react";
import { type FC, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";

/**
 * Loads older events as the reader scrolls near the top of what is loaded,
 * with the scroll anchoring that requires: prepending grows the content
 * above the viewport, and the browser keeps `scrollTop`, so the message the
 * reader was looking at has to be put back where it was.
 *
 * Must render inside `ThreadPrimitive.Viewport`, which provides the viewport
 * store this reads the scroll element from.
 */
export const LoadOlderEvents: FC<{
	hasOlder: boolean;
	isLoadingOlder: boolean;
	loadOlder: () => void;
}> = ({ hasOlder, isLoadingOlder, loadOlder }) => {
	const { t } = useTranslation("projects");
	const viewport = useThreadViewport((s) => s.element.viewport);
	const sentinelRef = useRef<HTMLDivElement | null>(null);
	// Distance from the end of the content, which older events do not change.
	// Message ids cannot be used: a message's id is the id of the first event in
	// its group, so prepending renames the group it extends.
	const anchorRef = useRef<number | null>(null);
	const isLoadingOlderRef = useRef(isLoadingOlder);
	isLoadingOlderRef.current = isLoadingOlder;

	const triggerLoadOlder = () => {
		if (viewport)
			anchorRef.current = viewport.scrollHeight - viewport.scrollTop;
		loadOlder();
	};

	// Prepended markdown and tool cards keep resizing for several frames after
	// they commit, so the correction has to survive more than one pass.
	useEffect(() => {
		const content = viewport?.firstElementChild;
		if (!viewport || !content) return;

		let deferred = 0;
		const apply = () => {
			const fromEnd = anchorRef.current;
			if (fromEnd === null) return;
			const target = viewport.scrollHeight - fromEnd;
			if (Math.abs(viewport.scrollTop - target) < 0.5) {
				if (!isLoadingOlderRef.current) anchorRef.current = null;
				return;
			}
			// The viewport sets `scroll-smooth`, which would animate this.
			const previous = viewport.style.scrollBehavior;
			viewport.style.scrollBehavior = "auto";
			viewport.scrollTop = target;
			viewport.style.scrollBehavior = previous;
		};
		const restore = () => {
			apply();
			// This observer belongs to a child of the viewport, so it runs before
			// the viewport's own resize handling. Re-apply after it.
			if (deferred) cancelAnimationFrame(deferred);
			deferred = requestAnimationFrame(() => {
				deferred = 0;
				apply();
			});
		};
		// A deliberate scroll means the reader, not us, decides the position —
		// but only once a fetch is no longer in flight. The load itself is now
		// triggered by scrolling near the top, so the same gesture's momentum
		// would otherwise cancel the anchor it just set before the response
		// even lands.
		const release = () => {
			if (!isLoadingOlderRef.current) anchorRef.current = null;
		};

		const observer = new ResizeObserver(restore);
		observer.observe(content);
		viewport.addEventListener("pointerdown", release);
		viewport.addEventListener("wheel", release, { passive: true });
		viewport.addEventListener("keydown", release);
		return () => {
			observer.disconnect();
			if (deferred) cancelAnimationFrame(deferred);
			viewport.removeEventListener("pointerdown", release);
			viewport.removeEventListener("wheel", release);
			viewport.removeEventListener("keydown", release);
		};
	}, [viewport]);

	// Tracks whether the sentinel above the oldest loaded message is on
	// screen — rootMargin fires it a little before the reader hits the
	// literal top, so the next page is already loading by the time they get
	// there instead of hitting a dead stop.
	const [sentinelVisible, setSentinelVisible] = useState(false);
	useEffect(() => {
		const sentinel = sentinelRef.current;
		if (!viewport || !sentinel || !hasOlder) return;
		const observer = new IntersectionObserver(
			([entry]) => setSentinelVisible(entry.isIntersecting),
			{ root: viewport, rootMargin: "150px 0px 0px 0px" },
		);
		observer.observe(sentinel);
		return () => observer.disconnect();
	}, [viewport, hasOlder]);

	// Fires on becoming visible, and again each time a fetch completes while
	// still visible — covers both "scrolled to the top" and "stayed at the
	// top through several consecutive pages".
	// biome-ignore lint/correctness/useExhaustiveDependencies: triggerLoadOlder is a new closure every render (it reads viewport) — depending on it would refire this effect on every render instead of only on the visibility/hasOlder/isLoadingOlder transitions it actually cares about.
	useEffect(() => {
		if (sentinelVisible && hasOlder && !isLoadingOlder) triggerLoadOlder();
	}, [sentinelVisible, hasOlder, isLoadingOlder]);

	if (!hasOlder) return null;

	return (
		<div ref={sentinelRef} className="flex justify-center py-2">
			{isLoadingOlder && (
				<div className="flex items-center gap-1.5 text-muted-foreground text-xs">
					<Loader2 className="size-3 animate-spin" />
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
 * Must render inside `ThreadPrimitive.Viewport`.
 */
export const TailFollowIndicator: FC<{
	newBelow: number;
	following: boolean;
	setFollowing: (following: boolean) => void;
	jumpToLatest: () => void;
}> = ({ newBelow, following, setFollowing, jumpToLatest }) => {
	const { t } = useTranslation("projects");
	const isAtBottom = useThreadViewport((s) => s.isAtBottom);
	const scrollToBottom = useThreadViewport((s) => s.scrollToBottom);

	useEffect(() => {
		if (isAtBottom !== following) setFollowing(isAtBottom);
	}, [isAtBottom, following, setFollowing]);

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
				onClick={() => {
					jumpToLatest();
					scrollToBottom({ behavior: "auto" });
				}}
			>
				<ArrowDown className="size-3" />
				{t("agents.conversationView.newBelow", { count: newBelow })}
			</Button>
		</div>
	);
};
