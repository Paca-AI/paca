import type { PageAnnotation } from "../shared/types";
import {
	CHECK_CIRCLE_ICON_SVG,
	CHECK_ICON_SVG,
	CLOSE_ICON_SVG,
	COPY_ICON_SVG,
	EXTERNAL_LINK_ICON_SVG,
	MESSAGE_SQUARE_ICON_SVG,
	PLUS_ICON_SVG,
	ROTATE_CCW_ICON_SVG,
	SEND_ICON_SVG,
	SQUARE_CHECK_BIG_ICON_SVG,
} from "./icons";
import { PACA_LOGO_DARK_SVG, PACA_LOGO_LIGHT_SVG } from "./logo";
import { resolveElement } from "./selector";
import { STYLES } from "./styles";

/** One on-page pin. Usually one comment thread, but threads whose on-screen
 * positions land within CLUSTER_RADIUS_PX of each other are grouped into a
 * single pin here rather than stacking separate, hard-to-click pins on top
 * of or beside each other — see setAnnotations. Hovering a multi-thread pin
 * fans its members back out into individually clickable pins (renderPins). */
export interface PinPlacement {
	annotations: PageAnnotation[];
	el: Element | null;
	approximate: boolean;
	pinEl?: HTMLElement;
}

export interface PanelHandlers {
	onResolve: (annotation: PageAnnotation) => void;
	onReopen: (annotation: PageAnnotation) => void;
	onReply: (annotation: PageAnnotation, body: string) => void;
	onCopyLink: (annotation: PageAnnotation) => Promise<boolean>;
	/** Opens this annotation's own comment detail page in a new tab —
	 * unlike the toolbar's old "Open in Paca" link (removed: it only ever
	 * jumped to the port forward's whole Comments tab), this goes straight
	 * to the one thread the popover is already showing. */
	onOpen: (annotation: PageAnnotation) => void;
	onCreateTask: (annotation: PageAnnotation) => void;
	onCreateConversation: (annotation: PageAnnotation) => void;
}

export interface ComposerResult {
	body: string;
}

// Two comments land in the same pin whenever their on-screen positions are
// within this many pixels of each other — comments on the exact same
// element are distance 0 and always cluster, but this also catches ones on
// merely adjacent elements that would otherwise render as separate pins
// stacked too close to tell apart or click individually.
const CLUSTER_RADIUS_PX = 24;

// How far each individual pin fans out from the cluster's shared center
// once expanded (see buildPin/renderPins) — comments on the exact same
// element resolve to the exact same point, so this is the only thing that
// ever visually separates them; it's applied uniformly rather than only
// when needed so the fan-out is predictable regardless of how close the
// underlying elements actually are.
const CLUSTER_FAN_RADIUS_PX = 26;

/** Owns the extension's entire on-page UI: a shadow-DOM overlay covering
 * the viewport, containing the hover-reveal toolbar, element-picking
 * highlight, rendered pins, and the comment composer / pin popover panel.
 * Kept as one class so the handful of overlapping concerns (positions that
 * all need to be recomputed together on scroll/resize) share state
 * naturally instead of needing their own synchronization. */
export class PacaOverlay {
	private host: HTMLElement;
	private shadow: ShadowRoot;
	private toolbarCommentBtn: HTMLButtonElement;
	private toolbarCountEl: HTMLElement;
	private highlightEl: HTMLElement;
	private pinsLayer: HTMLElement;
	private panelEl: HTMLElement | null = null;

	private commentMode = false;
	private showResolved = false;
	private placements: PinPlacement[] = [];
	private onPickCallback: ((el: Element) => void) | null = null;
	private onPinClickCallback: ((placement: PinPlacement) => void) | null = null;

	constructor() {
		this.host = document.createElement("div");
		this.host.setAttribute("data-paca-overlay", "");
		this.shadow = this.host.attachShadow({ mode: "open" });

		const style = document.createElement("style");
		style.textContent = STYLES;
		this.shadow.appendChild(style);

		this.pinsLayer = div("pins-layer");
		this.shadow.appendChild(this.pinsLayer);

		this.highlightEl = div("highlight");
		this.shadow.appendChild(this.highlightEl);

		const { wrap, commentBtn, countEl } = this.buildToolbar();
		this.toolbarCommentBtn = commentBtn;
		this.toolbarCountEl = countEl;
		this.shadow.appendChild(wrap);

		document.documentElement.appendChild(this.host);

		window.addEventListener("scroll", this.reposition, true);
		window.addEventListener("resize", this.reposition);
	}

	private buildToolbar() {
		const wrap = div("toolbar-wrap");
		const handle = div("toolbar-handle");
		const toolbar = div("toolbar");

		const brand = document.createElement("span");
		brand.className = "brand";
		brand.innerHTML = `<span class="logo-light">${PACA_LOGO_LIGHT_SVG}</span><span class="logo-dark">${PACA_LOGO_DARK_SVG}</span><span>Paca</span>`;

		const commentBtn = button("Comment", "btn-ghost", () =>
			this.setCommentMode(!this.commentMode),
		);

		const countEl = document.createElement("span");
		countEl.className = "count";
		countEl.textContent = "0";

		const sep = div("sep");

		let commentsVisible = true;
		const visibilityBtn = button("Hide comments", "btn-ghost", () => {
			commentsVisible = !commentsVisible;
			visibilityBtn.textContent = commentsVisible
				? "Hide comments"
				: "Show comments";
			visibilityBtn.classList.toggle("toggled", !commentsVisible);
			this.pinsLayer.style.display = commentsVisible ? "" : "none";
			if (!commentsVisible) this.closePanel();
		});

		const resolvedBtn = button("Show resolved", "btn-ghost", () => {
			this.showResolved = !this.showResolved;
			resolvedBtn.classList.toggle("toggled", this.showResolved);
			this.renderPins();
		});

		toolbar.append(brand, commentBtn, countEl, sep, visibilityBtn, resolvedBtn);
		wrap.append(handle, toolbar);
		return { wrap, commentBtn, countEl, resolvedBtn };
	}

	setCommentMode(on: boolean): void {
		this.commentMode = on;
		this.toolbarCommentBtn.classList.toggle("active", on);
		this.host.style.cursor = on ? "crosshair" : "";
		if (on) {
			document.addEventListener("mousemove", this.handleMouseMove, true);
			document.addEventListener("click", this.handleClick, true);
		} else {
			document.removeEventListener("mousemove", this.handleMouseMove, true);
			document.removeEventListener("click", this.handleClick, true);
			this.highlightEl.style.display = "none";
		}
	}

	onElementPicked(cb: (el: Element) => void): void {
		this.onPickCallback = cb;
	}

	onPinClicked(cb: (placement: PinPlacement) => void): void {
		this.onPinClickCallback = cb;
	}

	private isInsideOverlay(target: EventTarget | null): boolean {
		return target === this.host;
	}

	private handleMouseMove = (e: MouseEvent): void => {
		const target = e.target;
		if (this.isInsideOverlay(target)) return;
		this.showHighlight(target as Element);
	};

	private handleClick = (e: MouseEvent): void => {
		const target = e.target;
		if (this.isInsideOverlay(target)) return;
		e.preventDefault();
		e.stopPropagation();
		const el = target as Element;
		this.setCommentMode(false);
		this.onPickCallback?.(el);
	};

	private showHighlight(el: Element): void {
		const rect = el.getBoundingClientRect();
		Object.assign(this.highlightEl.style, {
			display: "block",
			left: `${rect.left}px`,
			top: `${rect.top}px`,
			width: `${rect.width}px`,
			height: `${rect.height}px`,
		});
	}

	setPinCount(open: number): void {
		this.toolbarCountEl.textContent = String(open);
	}

	setAnnotations(annotations: PageAnnotation[]): void {
		// Resolve each annotation to its own current on-screen position first
		// (same el-or-bounding-box-percentage fallback rectFor uses below),
		// then cluster by that position rather than by recorded selector —
		// comments on the exact same element land at the exact same point
		// (distance 0, always clustered), and this also catches comments on
		// merely adjacent elements that would otherwise render as separate,
		// hard-to-tell-apart pins on top of or right next to each other.
		const resolvedItems = annotations.map((annotation) => {
			const resolved = resolveElement(
				annotation.element_selector,
				annotation.element_selector_fallbacks,
				annotation.element_snapshot.text_excerpt,
			);
			const el = resolved?.el ?? null;
			let x: number;
			let y: number;
			if (el?.isConnected) {
				const r = el.getBoundingClientRect();
				x = r.left;
				y = r.top;
			} else {
				const bbox = annotation.bounding_box;
				x = (bbox.x_pct / 100) * window.innerWidth;
				y = (bbox.y_pct / 100) * window.innerHeight;
			}
			return {
				annotation,
				el,
				approximate: resolved?.approximate ?? true,
				x,
				y,
			};
		});

		const clusters: {
			items: typeof resolvedItems;
			x: number;
			y: number;
		}[] = [];
		for (const item of resolvedItems) {
			const cluster = clusters.find(
				(c) => Math.hypot(c.x - item.x, c.y - item.y) <= CLUSTER_RADIUS_PX,
			);
			if (cluster) cluster.items.push(item);
			else clusters.push({ items: [item], x: item.x, y: item.y });
		}

		this.placements = clusters.map((cluster) => ({
			annotations: cluster.items.map((i) => i.annotation),
			el: cluster.items[0].el,
			approximate: cluster.items[0].approximate,
		}));
		this.setPinCount(annotations.filter((a) => a.status === "open").length);
		this.renderPins();
	}

	/** One pin's clickable badge — the merged center pin (annotations = every
	 * currently-visible thread in the cluster, for its combined
	 * avatar/color/badge/title) or one fanned-out satellite (annotations = a
	 * single thread, once the user has hovered to tell the cluster's threads
	 * apart). Clicking either kind fires the same callback with just the
	 * annotations it represents, so a satellite click opens only its own
	 * thread's popover. Callers filter by showResolved before calling this —
	 * it just renders whatever list it's given. */
	private buildPin(
		placement: PinPlacement,
		annotations: PageAnnotation[],
	): HTMLElement {
		const resolved = annotations.every((a) => a.status === "resolved");
		const pin = document.createElement("div");
		pin.className = "pin";
		if (resolved) pin.classList.add("resolved");
		if (placement.approximate) pin.classList.add("approximate");

		// Figma tints each pin with its author's own identity color and shows
		// their avatar — always whoever STARTED the thread, even after others
		// reply, exactly like Figma's own pins never change to show the
		// latest replier. For a pin grouping more than one thread (a Paca
		// addition Figma has no equivalent of), the first thread's starter
		// stands in for the whole merged pin.
		const primary = annotations[0];
		if (!resolved) pin.style.setProperty("--pin-color", identityColor(primary));
		pin.appendChild(renderAvatarContent(primary));

		// Total messages (this pin's own thread's original comment + its
		// replies, or every thread's when this is the merged center pin) — a
		// Paca addition Figma's own pins don't have.
		const total = annotations.reduce(
			(sum, a) => sum + 1 + a.comments.length,
			0,
		);
		if (total > 1) {
			const badge = document.createElement("span");
			badge.className = "pin-badge";
			badge.textContent = String(total);
			pin.appendChild(badge);
		}

		pin.title =
			annotations.length > 1
				? `${annotations.length} comments`
				: annotations[0].body.slice(0, 80);
		pin.addEventListener("click", (e) => {
			e.stopPropagation();
			this.onPinClickCallback?.({ ...placement, annotations });
		});
		return pin;
	}

	private renderPins(): void {
		this.pinsLayer.innerHTML = "";
		for (const placement of this.placements) {
			// Filtered once and reused everywhere below (center pin, badge
			// count, satellites, and the annotations a click hands off to the
			// popover) — a resolved thread has replies of its own, but that's
			// still not a reason for it to ignore "Show resolved": a satellite
			// built straight from placement.annotations, unfiltered, used to
			// fan a resolved thread back out (and a center-pin click used to
			// open it) even with the toggle off.
			const visible = this.showResolved
				? placement.annotations
				: placement.annotations.filter((a) => a.status !== "resolved");
			if (visible.length === 0) continue;

			const cluster = div("pin-cluster");
			const center = this.buildPin(placement, visible);
			center.classList.add("pin-center");
			center.style.transform = "translate(-50%, -50%)";
			cluster.appendChild(center);

			// More than one VISIBLE thread landed on this spot — hovering
			// anywhere near the pin (see the generous .has-satellites hit zone
			// in styles.ts) fans each thread's own pin out around the same
			// center point so it can be picked individually, instead of only
			// ever opening the combined popover.
			if (visible.length > 1) {
				cluster.classList.add("has-satellites");
				const satellites = div("pin-satellites");
				const n = visible.length;
				visible.forEach((annotation, i) => {
					const angle = (2 * Math.PI * i) / n - Math.PI / 2;
					const dx = Math.round(CLUSTER_FAN_RADIUS_PX * Math.cos(angle));
					const dy = Math.round(CLUSTER_FAN_RADIUS_PX * Math.sin(angle));
					const satellite = this.buildPin(placement, [annotation]);
					satellite.style.transform = `translate(-50%, -50%) translate(${dx}px, ${dy}px)`;
					satellites.appendChild(satellite);
				});
				cluster.appendChild(satellites);
			}

			this.pinsLayer.appendChild(cluster);
			placement.pinEl = cluster;
		}
		this.reposition();
	}

	private reposition = (): void => {
		if (this.commentMode) this.highlightEl.style.display = "none"; // recomputed on next mousemove
		for (const placement of this.placements) {
			if (!placement.pinEl) continue;
			const rect = this.rectFor(placement);
			if (!rect) {
				placement.pinEl.style.display = "none";
				continue;
			}
			placement.pinEl.style.display = "";
			placement.pinEl.style.left = `${rect.left}px`;
			placement.pinEl.style.top = `${rect.top}px`;
		}
	};

	private rectFor(
		placement: PinPlacement,
	): { left: number; top: number } | null {
		if (placement.el?.isConnected) {
			const r = placement.el.getBoundingClientRect();
			return { left: r.left, top: r.top };
		}
		// Fall back to the stored percentage position relative to the current
		// viewport size — approximate, but keeps the pin roughly where it was
		// even when the element itself can no longer be found. Every
		// annotation in the group was placed on the same element, so any of
		// them gives the same position.
		const bbox = placement.annotations[0].bounding_box;
		return {
			left: (bbox.x_pct / 100) * window.innerWidth,
			top: (bbox.y_pct / 100) * window.innerHeight,
		};
	}

	/** Builds the composer's header/close-button chrome — a labeled title
	 * bar makes sense there since it's a one-off panel a user may not have
	 * seen before. The pin popover instead uses buildFloatingClose below: a
	 * bare close button with no title bar, matching Figma's own
	 * chrome-less comment panel. Both still get the outside-click/Escape
	 * dismissal openPanel wires up regardless. */
	private buildPanelHeader(title: string): HTMLElement {
		const header = div("panel-header");
		const titleEl = document.createElement("span");
		titleEl.textContent = title;
		const closeBtn = document.createElement("button");
		closeBtn.className = "panel-close";
		closeBtn.setAttribute("aria-label", "Close");
		closeBtn.innerHTML = CLOSE_ICON_SVG;
		closeBtn.addEventListener("click", () => this.closePanel());
		header.append(titleEl, closeBtn);
		return header;
	}

	private buildFloatingClose(): HTMLElement {
		const wrap = div("panel-close-wrap");
		const closeBtn = document.createElement("button");
		closeBtn.className = "panel-close";
		closeBtn.setAttribute("aria-label", "Close");
		closeBtn.innerHTML = CLOSE_ICON_SVG;
		closeBtn.addEventListener("click", () => this.closePanel());
		wrap.appendChild(closeBtn);
		return wrap;
	}

	/** Shows panel, wiring up the dismissal paths every panel shares:
	 * clicking outside it (composedPath, since the click may originate
	 * inside the shadow tree) and pressing Escape. Registered fresh per
	 * panel rather than once up front so a stray listener never outlives
	 * the panel it belonged to. */
	private openPanel(panel: HTMLElement, anchorRect: DOMRect): void {
		this.closePanel();
		// Appended before positioning so positionPanel can read the panel's
		// real rendered size (a detached element has none) — needed to know
		// how far it overflows the viewport, not just where it starts.
		this.shadow.appendChild(panel);
		this.positionPanel(panel, anchorRect);
		this.panelEl = panel;
		// Deferred one tick so the click that opened this panel (which is
		// still bubbling right now) doesn't immediately close it again.
		setTimeout(() => {
			document.addEventListener("click", this.handleOutsideClick, true);
			document.addEventListener("keydown", this.handleEscKey, true);
		}, 0);
	}

	private handleOutsideClick = (e: MouseEvent): void => {
		if (!this.panelEl) return;
		if (e.composedPath().includes(this.panelEl)) return;
		this.closePanel();
	};

	private handleEscKey = (e: KeyboardEvent): void => {
		if (e.key === "Escape") this.closePanel();
	};

	/** New-comment box: the same rounded pill + send button as a thread's own
	 * reply row (renderThread), matching Figma exactly — new comment and
	 * reply are the identical control there too. Dismissing without
	 * posting has no separate Cancel button, again as in Figma: the close
	 * icon, clicking outside, and Escape (all wired by openPanel) already
	 * cover it. */
	showComposer(
		anchorRect: DOMRect,
		onSubmit: (result: ComposerResult) => void,
	): void {
		const panel = div("panel");
		panel.appendChild(this.buildPanelHeader("New comment"));
		const body = div("panel-body");

		const replyRow = div("reply-row");
		const textarea = document.createElement("textarea");
		textarea.placeholder = "Leave a comment…";
		textarea.rows = 1;
		const sendBtn = document.createElement("button");
		sendBtn.className = "send-btn";
		sendBtn.innerHTML = SEND_ICON_SVG;
		sendBtn.setAttribute("aria-label", "Post comment");
		const submit = () => {
			const value = textarea.value.trim();
			if (!value) return;
			this.closePanel();
			onSubmit({ body: value });
		};
		sendBtn.addEventListener("click", submit);
		textarea.addEventListener("keydown", (e) => {
			if (e.key === "Enter" && !e.shiftKey) {
				e.preventDefault();
				submit();
			}
		});
		replyRow.append(textarea, sendBtn);
		body.appendChild(replyRow);

		panel.appendChild(body);
		this.openPanel(panel, anchorRect);
		textarea.focus();
	}

	/** One thread's worth of the pin popover: a header with the actions that
	 * apply to THIS thread specifically (resolving or copying a link to one
	 * thread on a shared pin must never touch the others anchored to the
	 * same element), then its comment + replies, then a reply box. */
	private renderThread(
		annotation: PageAnnotation,
		handlers: PanelHandlers,
	): HTMLElement {
		const thread = div("thread");

		const resolved = annotation.status === "resolved";
		// Icon-only throughout (title/aria-label carry the text instead) —
		// five actions in one row leaves no room for text labels without
		// wrapping or forcing the popover wider than Figma's own comment
		// panel ever is.
		const header = div("thread-header");
		header.appendChild(
			iconButton(
				resolved ? ROTATE_CCW_ICON_SVG : CHECK_CIRCLE_ICON_SVG,
				resolved ? "Reopen" : "Resolve",
				"btn-secondary",
				() =>
					resolved
						? handlers.onReopen(annotation)
						: handlers.onResolve(annotation),
			),
		);
		const copyBtn = iconButton(
			COPY_ICON_SVG,
			"Copy link",
			"btn-secondary",
			() => {
				void handlers.onCopyLink(annotation).then((ok) => {
					// Transient confirmation, plain DOM (no React here) — mirrors
					// the web app's own "Copied!" 2-second label swap
					// (task-header.tsx), just as an icon swap + tooltip text
					// instead of a label swap. Only claims success once the copy
					// actually landed -- see clipboard.ts for why it can fail.
					copyBtn.innerHTML = ok ? CHECK_ICON_SVG : COPY_ICON_SVG;
					copyBtn.title = ok ? "Copied!" : "Copy failed";
					setTimeout(() => {
						copyBtn.innerHTML = COPY_ICON_SVG;
						copyBtn.title = "Copy link";
					}, 2000);
				});
			},
		);
		header.appendChild(copyBtn);
		header.appendChild(
			iconButton(
				EXTERNAL_LINK_ICON_SVG,
				"Open comment page",
				"btn-secondary",
				() => handlers.onOpen(annotation),
			),
		);
		const createItems: DropdownItem[] = annotation.task_id
			? [
					{
						icon: SQUARE_CHECK_BIG_ICON_SVG,
						label: "Task created",
						onClick: () => {},
						disabled: true,
					},
				]
			: [
					{
						icon: SQUARE_CHECK_BIG_ICON_SVG,
						label: "Create task",
						onClick: () => handlers.onCreateTask(annotation),
					},
				];
		createItems.push({
			icon: MESSAGE_SQUARE_ICON_SVG,
			label: "Create conversation",
			onClick: () => handlers.onCreateConversation(annotation),
		});
		header.appendChild(buildDropdown(PLUS_ICON_SVG, "Create", createItems));
		thread.appendChild(header);

		// Its own scroll area, separate from the header above and the reply
		// row below — scrolling through a long thread should never push
		// either out of view.
		const commentsList = div("thread-comments");
		commentsList.appendChild(renderCommentItem(annotation));
		for (const c of annotation.comments)
			commentsList.appendChild(renderCommentItem(c));
		thread.appendChild(commentsList);

		const replyRow = div("reply-row");
		const replyInput = document.createElement("textarea");
		replyInput.placeholder = "Reply…";
		replyInput.rows = 1;
		const sendBtn = document.createElement("button");
		sendBtn.className = "send-btn";
		sendBtn.innerHTML = SEND_ICON_SVG;
		sendBtn.setAttribute("aria-label", "Send reply");
		const sendReply = () => {
			const value = replyInput.value.trim();
			if (!value) return;
			handlers.onReply(annotation, value);
		};
		sendBtn.addEventListener("click", sendReply);
		replyInput.addEventListener("keydown", (e) => {
			if (e.key === "Enter" && !e.shiftKey) {
				e.preventDefault();
				sendReply();
			}
		});
		replyRow.append(replyInput, sendBtn);
		thread.appendChild(replyRow);

		return thread;
	}

	/** Shows the popover for one pin. annotations is every thread anchored to
	 * that element (see setAnnotations) — normally one, but when a second
	 * comment was started independently on the same element, both threads
	 * render here, each in its own clearly separated block with its own
	 * reply box and actions, instead of the first one being unreachable
	 * behind the second. No title bar, just a floating close button —
	 * Figma's own comment panel has no header text either. */
	showPinPopover(
		annotations: PageAnnotation[],
		anchorRect: DOMRect,
		handlers: PanelHandlers,
	): void {
		const panel = div("panel");
		panel.appendChild(this.buildFloatingClose());

		const body = div("panel-body");
		annotations.forEach((annotation, i) => {
			if (i > 0) body.appendChild(div("thread-divider"));
			body.appendChild(this.renderThread(annotation, handlers));
		});
		panel.appendChild(body);

		this.openPanel(panel, anchorRect);
	}

	closePanel(): void {
		this.panelEl?.remove();
		this.panelEl = null;
		document.removeEventListener("click", this.handleOutsideClick, true);
		document.removeEventListener("keydown", this.handleEscKey, true);
	}

	private positionPanel(panel: HTMLElement, anchorRect: DOMRect): void {
		// Anchored at the element's top-left by default, then nudged up
		// and/or left by exactly however much it would otherwise overflow —
		// using the panel's own measured size rather than a guessed one, so
		// it moves the minimum needed to stay fully on screen.
		let top = anchorRect.top;
		const bottomOverflow = top + panel.offsetHeight - (window.innerHeight - 8);
		if (bottomOverflow > 0) top -= bottomOverflow;

		let left = anchorRect.left;
		const rightOverflow = left + panel.offsetWidth - (window.innerWidth - 8);
		if (rightOverflow > 0) left -= rightOverflow;

		panel.style.top = `${Math.max(8, top)}px`;
		panel.style.left = `${Math.max(8, left)}px`;
	}

	/** Hides every bit of Paca's own on-page UI (toolbar, pins, highlight,
	 * any open panel) — used to keep chrome.tabs.captureVisibleTab's
	 * screenshot showing only the page itself, not our own overlay, since
	 * that capture sees whatever is actually rendered in the tab and this
	 * host element is very much part of that. Pair with show() once the
	 * capture is done. */
	hide(): void {
		this.host.style.display = "none";
	}

	show(): void {
		this.host.style.display = "";
	}

	destroy(): void {
		this.setCommentMode(false);
		window.removeEventListener("scroll", this.reposition, true);
		window.removeEventListener("resize", this.reposition);
		this.host.remove();
	}
}

function div(className: string): HTMLDivElement {
	const el = document.createElement("div");
	el.className = className;
	return el;
}

function button(
	label: string,
	variantClassName: string,
	onClick: () => void,
): HTMLButtonElement {
	const btn = document.createElement("button");
	btn.textContent = label;
	btn.className = `btn ${variantClassName}`;
	btn.addEventListener("click", onClick);
	return btn;
}

/** button()'s icon-only counterpart — ariaLabel doubles as the tooltip
 * (title), since there's no visible text to explain what the icon means. */
function iconButton(
	iconSvg: string,
	ariaLabel: string,
	variantClassName: string,
	onClick: () => void,
): HTMLButtonElement {
	const btn = document.createElement("button");
	btn.type = "button";
	btn.innerHTML = iconSvg;
	btn.className = `btn btn-icon ${variantClassName}`;
	btn.title = ariaLabel;
	btn.setAttribute("aria-label", ariaLabel);
	btn.addEventListener("click", onClick);
	return btn;
}

interface DropdownItem {
	icon: string;
	label: string;
	onClick: () => void;
	disabled?: boolean;
}

/** A small anchored menu opened from an icon-only trigger button — used by
 * the thread header's Create action (renderThread), the one action here
 * that needs a choice (task vs. conversation) rather than a single click.
 * Self-contained: each instance owns its own open/close state and its own
 * outside-click listener, independent of the panel's own (see openPanel/
 * handleOutsideClick) — a click anywhere inside the panel but outside this
 * dropdown closes just the dropdown, never the whole panel. */
function buildDropdown(
	triggerIconSvg: string,
	ariaLabel: string,
	items: DropdownItem[],
): HTMLElement {
	const wrap = div("dropdown");
	const trigger = document.createElement("button");
	trigger.type = "button";
	trigger.innerHTML = triggerIconSvg;
	trigger.className = "btn btn-icon btn-secondary";
	trigger.title = ariaLabel;
	trigger.setAttribute("aria-label", ariaLabel);

	const menu = div("dropdown-menu");
	menu.style.display = "none";

	const onOutsideClick = (e: MouseEvent) => {
		if (e.composedPath().includes(wrap)) return;
		close();
	};
	function open() {
		menu.style.display = "block";
		setTimeout(
			() => document.addEventListener("click", onOutsideClick, true),
			0,
		);
	}
	function close() {
		menu.style.display = "none";
		document.removeEventListener("click", onOutsideClick, true);
	}

	trigger.addEventListener("click", (e) => {
		e.stopPropagation();
		if (menu.style.display === "none") open();
		else close();
	});

	for (const item of items) {
		const row = document.createElement("button");
		row.type = "button";
		row.className = "dropdown-item";
		row.disabled = Boolean(item.disabled);
		const iconSpan = document.createElement("span");
		iconSpan.innerHTML = item.icon;
		const labelSpan = document.createElement("span");
		labelSpan.textContent = item.label;
		row.append(iconSpan, labelSpan);
		if (!item.disabled) {
			row.addEventListener("click", (e) => {
				e.stopPropagation();
				close();
				item.onClick();
			});
		}
		menu.appendChild(row);
	}

	wrap.append(trigger, menu);
	return wrap;
}

/** Shape shared by PageAnnotation and AnnotationComment — enough to render
 * one entry in a comment thread (the top-level annotation body counts as
 * the thread's first entry) without caring which one it actually is. */
interface AuthoredContent {
	body: string;
	created_by_name: string;
	created_by_username: string;
	created_by_avatar_url?: string;
	created_by_avatar_thumb_url?: string;
	created_at: string;
}

function renderCommentItem(item: AuthoredContent): HTMLElement {
	const el = div("comment-item");

	const header = div("comment-header");
	header.appendChild(renderAvatar(item));

	const author = document.createElement("span");
	author.className = "comment-author";
	author.textContent = item.created_by_name || item.created_by_username;
	header.appendChild(author);

	const meta = document.createElement("span");
	meta.className = "comment-meta";
	meta.textContent = formatRelativeTime(item.created_at);
	meta.title = new Date(item.created_at).toLocaleString();
	header.appendChild(meta);

	const bodyEl = div("comment-body");
	bodyEl.textContent = item.body;

	el.append(header, bodyEl);
	return el;
}

// A fixed, deterministically-assigned-per-person palette — Paca's stand-in
// for Figma's own per-collaborator identity colors (used there for cursors,
// avatar rings, and pins alike), since Paca has no server-side concept of
// "this member's color" to read instead.
const IDENTITY_COLORS = [
	"#e35d4a",
	"#e0862f",
	"#c9a227",
	"#5a9e1c",
	"#1c9e6e",
	"#1c8fa0",
	"#3d6fd6",
	"#8b5cf6",
	"#c2419e",
];

function identityColor(item: AuthoredContent): string {
	const seed = item.created_by_username || item.created_by_name || "";
	let hash = 0;
	for (let i = 0; i < seed.length; i++)
		hash = (hash * 31 + seed.charCodeAt(i)) | 0;
	return IDENTITY_COLORS[Math.abs(hash) % IDENTITY_COLORS.length];
}

function initialLetter(item: AuthoredContent): string {
	const name = item.created_by_name || item.created_by_username;
	return name ? name.charAt(0).toUpperCase() : "?";
}

/** The avatar image or initial letter alone, with no wrapper — used both
 * inside the fixed-size circular .avatar (comment list) and directly inside
 * the teardrop-shaped pin, which needs the same content in a differently
 * shaped, differently sized container. */
function renderAvatarContent(item: AuthoredContent): Node {
	const avatarUrl =
		item.created_by_avatar_thumb_url ?? item.created_by_avatar_url;
	if (avatarUrl) {
		const img = document.createElement("img");
		img.src = avatarUrl;
		img.alt = "";
		return img;
	}
	return document.createTextNode(initialLetter(item));
}

function renderAvatar(item: AuthoredContent): HTMLElement {
	const avatar = div("avatar");
	if (!(item.created_by_avatar_thumb_url ?? item.created_by_avatar_url)) {
		const color = identityColor(item);
		avatar.style.background = color;
		avatar.style.color = "#fff";
	}
	avatar.appendChild(renderAvatarContent(item));
	return avatar;
}

/** Compact relative timestamp ("2m", "3h", "5d"), matching Figma's own
 * comment timestamps — the full absolute time is still available as a
 * title tooltip on hover, so nothing precise is lost. */
function formatRelativeTime(iso: string): string {
	const date = new Date(iso);
	const diffSec = Math.round((Date.now() - date.getTime()) / 1000);
	if (diffSec < 60) return "now";
	const diffMin = Math.round(diffSec / 60);
	if (diffMin < 60) return `${diffMin}m`;
	const diffHour = Math.round(diffMin / 60);
	if (diffHour < 24) return `${diffHour}h`;
	const diffDay = Math.round(diffHour / 24);
	if (diffDay < 7) return `${diffDay}d`;
	return date.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}
