// Element selector generation + re-matching. A page under active
// development can have its DOM structure change between visits — the
// whole point of storing multiple fallback strategies (plus a bounding-box
// percentage as a last resort, handled by the caller) is that a pin should
// degrade gracefully instead of silently attaching to the wrong element.

const STABLE_ATTRS = ["data-testid", "data-test", "data-cy", "data-qa"];
const MAX_STRUCTURAL_DEPTH = 8;
const MAX_ANCESTOR_HOPS = 6;

function cssEscape(value: string): string {
	if (typeof CSS !== "undefined" && typeof CSS.escape === "function")
		return CSS.escape(value);
	return value.replace(/[^a-zA-Z0-9_-]/g, (c) => `\\${c}`);
}

function attrSelector(el: Element): string | null {
	for (const attr of STABLE_ATTRS) {
		const value = el.getAttribute(attr);
		if (value) return `[${attr}="${cssEscape(value)}"]`;
	}
	if (el.id) return `#${cssEscape(el.id)}`;
	return null;
}

function nthOfTypeSegment(el: Element): string {
	const tag = el.tagName.toLowerCase();
	const parent = el.parentElement;
	if (!parent) return tag;
	const siblingsOfType = Array.from(parent.children).filter(
		(c) => c.tagName === el.tagName,
	);
	if (siblingsOfType.length <= 1) return tag;
	const index = siblingsOfType.indexOf(el) + 1;
	return `${tag}:nth-of-type(${index})`;
}

function relativePath(from: Element, to: Element): string {
	const parts: string[] = [];
	let node: Element | null = to;
	while (node && node !== from) {
		parts.unshift(nthOfTypeSegment(node));
		node = node.parentElement;
	}
	return parts.join(" > ");
}

function structuralPath(el: Element, maxDepth = MAX_STRUCTURAL_DEPTH): string {
	const parts: string[] = [];
	let node: Element | null = el;
	let depth = 0;
	while (
		node &&
		node !== document.body &&
		node.parentElement &&
		depth < maxDepth
	) {
		parts.unshift(nthOfTypeSegment(node));
		node = node.parentElement;
		depth++;
	}
	return `body > ${parts.join(" > ")}`;
}

export interface GeneratedSelector {
	selector: string;
	fallbacks: string[];
}

/** Generates a primary selector plus fallbacks for el, preferring stable
 * test/id attributes over brittle structural paths. */
export function generateSelectors(el: Element): GeneratedSelector {
	const candidates: string[] = [];

	const own = attrSelector(el);
	if (own) candidates.push(own);

	let ancestor = el.parentElement;
	let hops = 0;
	while (ancestor && ancestor !== document.body && hops < MAX_ANCESTOR_HOPS) {
		const anchor = attrSelector(ancestor);
		if (anchor) {
			candidates.push(`${anchor} > ${relativePath(ancestor, el)}`);
			break;
		}
		ancestor = ancestor.parentElement;
		hops++;
	}

	candidates.push(structuralPath(el));

	const [selector, ...fallbacks] = candidates;
	return { selector: selector ?? structuralPath(el), fallbacks };
}

function textRoughlyMatches(el: Element, expectedText: string): boolean {
	const expected = expectedText.trim();
	if (!expected) return true;
	const actual = (el.textContent ?? "").trim().slice(0, 200);
	if (!actual) return false;
	return (
		actual.includes(expected) ||
		expected.includes(actual) ||
		actual.slice(0, 40) === expected.slice(0, 40)
	);
}

export interface ResolvedElement {
	el: Element;
	/** false only for the primary selector matching with a confirmed text match. */
	approximate: boolean;
}

/** Re-resolves a stored selector against the current page. Tries the
 * primary selector, then each fallback in order, verifying the resolved
 * element's text still roughly matches what was captured at comment time —
 * a selector that now matches unrelated content (e.g. after a page
 * refactor reused the same test id) is treated as a miss, not a false
 * match. Returns null if nothing resolves; the caller falls back to the
 * stored bounding-box percentage position instead. */
export function resolveElement(
	selector: string,
	fallbacks: string[],
	expectedTextExcerpt: string,
): ResolvedElement | null {
	const candidates = [selector, ...fallbacks];
	for (let i = 0; i < candidates.length; i++) {
		let el: Element | null = null;
		try {
			el = document.querySelector(candidates[i]);
		} catch {
			continue; // an invalid selector on this page (e.g. an escaping edge case)
		}
		if (el && textRoughlyMatches(el, expectedTextExcerpt)) {
			return { el, approximate: i > 0 };
		}
	}
	return null;
}

export function accessibleNameOf(el: Element): string {
	return (
		el.getAttribute("aria-label") ??
		el.getAttribute("alt") ??
		el.getAttribute("title") ??
		(el as HTMLElement).innerText?.trim().slice(0, 80) ??
		""
	);
}

export function roleOf(el: Element): string {
	return el.getAttribute("role") ?? el.tagName.toLowerCase();
}

export function outerHtmlExcerpt(el: Element, maxLength = 500): string {
	const clone = el.cloneNode(true) as Element;
	// Strip inline event handlers and any nested <script> before this is
	// ever stored/displayed — it's arbitrary page HTML, not markup Paca
	// should treat as safe to execute.
	for (const s of Array.from(clone.querySelectorAll("script"))) s.remove();
	for (const el2 of [clone, ...Array.from(clone.querySelectorAll("*"))]) {
		for (const attr of Array.from(el2.attributes)) {
			if (attr.name.toLowerCase().startsWith("on"))
				el2.removeAttribute(attr.name);
		}
	}
	return clone.outerHTML.slice(0, maxLength);
}
