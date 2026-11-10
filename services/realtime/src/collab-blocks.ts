// Merging a write that did not come through the editors (an agent, MCP, a
// script) into a live collaborative room.
//
// Editors persist by saving block JSON through the REST API; the room's Y.Doc
// is the live truth while they are connected. A REST write by anyone else is
// therefore invisible to the editors and is overwritten by their next save.
// The API announces every such write as `doc.updated` / `task.updated` with the
// field's old and new BlockNote JSON, and this module folds that change into
// the Y.Doc as a block-level three-way merge:
//
//   base   = the content before the write (event `old`)
//   theirs = the content after it          (event `new`)
//   ours   = what the room holds right now (the live Y.Doc)
//
// Per block (matched by id):
//   * added in theirs      -> inserted, unless the room already has that id
//   * changed in theirs    -> replaced, only while the room's copy still equals
//                             the base (nobody edited it live)
//   * removed in theirs    -> deleted, only while the room's copy and all its
//                             descendants are still untouched
// A block someone edited live is left alone ("ours wins"): the editor's save
// then stores their version. Edits to *other* blocks, cursors and selections
// are untouched, because only the affected block nodes are replaced.
//
// Blocks do not need ids in the stored JSON (the API only requires `type`), so
// when theirs / the base lack ids they are recovered by aligning siblings on
// content (see resolveIds). Moves/reorders are not merged (documented
// limitation).

import {
	BlockNoteEditor,
	BlockNoteSchema,
	createBlockSpec,
	createInlineContentSpec,
	defaultBlockSpecs,
	defaultInlineContentSpecs,
} from "@blocknote/core";
import {
	blocksToYDoc,
	blocksToYXmlFragment,
	yDocToBlocks,
} from "@blocknote/core/yjs";
import * as Y from "yjs";

export interface Blk {
	id: string;
	type: string;
	props?: Record<string, unknown>;
	content?: unknown;
	children: Blk[];
}

export interface MergeResult {
	// Operations applied to the Y.Doc (0 = nothing to broadcast).
	applied: number;
	// Changed blocks left alone because the room's copy was edited live.
	conflicts: number;
	// Set when the change could not be merged at all.
	skipped?: "unsupported" | "too_large" | "not_comparable";
}

// ── Schema ───────────────────────────────────────────────────────────────────
// Must name the same blocks / inline contents (and props) as the editors' schema
// in apps/web/src/components/shared/blocknote-schema.tsx; rendering is never
// used here, only the structure.

const NOOP = { render: () => ({ dom: undefined as never }) } as never;

function stubBlock<T extends string>(
	type: T,
	propSchema: Record<string, { default: string }>,
) {
	return createBlockSpec(
		{ type, propSchema, content: "none" } as never,
		NOOP,
	) as unknown as () => never;
}

function stubInline(
	type: string,
	propSchema: Record<string, { default: string | undefined }>,
) {
	return createInlineContentSpec(
		{ type, propSchema, content: "none" } as never,
		NOOP,
	) as never;
}

let cachedEditor: BlockNoteEditor | undefined;

function editor(): BlockNoteEditor {
	if (!cachedEditor) {
		const schema = BlockNoteSchema.create({
			blockSpecs: {
				...defaultBlockSpecs,
				mermaid: stubBlock("mermaid", { code: { default: "" } })(),
				annotationCard: stubBlock("annotationCard", {
					id: { default: "" },
					projectId: { default: "" },
					environmentId: { default: "" },
					portForwardId: { default: "" },
				})(),
			},
			inlineContentSpecs: {
				...defaultInlineContentSpecs,
				teamMention: stubInline("teamMention", {
					id: { default: "" },
					name: { default: "Unknown" },
					avatar: { default: undefined },
				}),
				taskReference: stubInline("taskReference", {
					id: { default: "" },
					title: { default: "Unknown" },
					status: { default: "open" },
				}),
				docReference: stubInline("docReference", {
					id: { default: "" },
					title: { default: "Unknown" },
				}),
			},
		});
		cachedEditor = BlockNoteEditor.create({
			schema,
		}) as unknown as BlockNoteEditor;
	}
	return cachedEditor;
}

// ── Canonical blocks ─────────────────────────────────────────────────────────

const NO_ID = "__noid:";
// The fragment the scratch conversions use; unrelated to the room's name.
const SCRATCH = "scratch";

function markMissingIds(blocks: unknown[], counter = { n: 0 }): unknown[] {
	return blocks.map((raw) => {
		const b = { ...(raw as Record<string, unknown>) };
		if (typeof b.id !== "string" || b.id === "") {
			b.id = `${NO_ID}${counter.n++}`;
		}
		if (Array.isArray(b.children))
			b.children = markMissingIds(b.children, counter);
		return b;
	});
}

// canon rewrites partial block JSON into the exact form the editors produce
// (defaults filled in, content as inline arrays), so it can be compared with the
// blocks read back from the Y.Doc. Blocks without an id keep a `__noid:` marker.
function canon(blocks: unknown[]): Blk[] {
	if (blocks.length === 0) return [];
	const ed = editor();
	const doc = blocksToYDoc(ed, markMissingIds(blocks) as never, SCRATCH);
	try {
		return yDocToBlocks(ed, doc, SCRATCH) as unknown as Blk[];
	} finally {
		doc.destroy();
	}
}

const hasId = (b: Blk): boolean => !b.id.startsWith(NO_ID);

// ownKey identifies a block's own content, ignoring its id and its children.
function ownKey(b: Blk): string {
	return JSON.stringify([b.type, b.props ?? {}, b.content ?? null]);
}

function* walk(blocks: Blk[]): Generator<Blk> {
	for (const b of blocks) {
		yield b;
		yield* walk(b.children);
	}
}

interface Node {
	block: Blk;
	parentId: string | null;
}

function index(blocks: Blk[], parentId: string | null, out: Map<string, Node>) {
	for (const b of blocks) {
		out.set(b.id, { block: b, parentId });
		index(b.children, b.id, out);
	}
}

function indexOf(blocks: Blk[]): Map<string, Node> {
	const m = new Map<string, Node>();
	index(blocks, null, m);
	return m;
}

// ── Recovering ids ───────────────────────────────────────────────────────────

const MAX_ALIGN_CELLS = 4_000_000;

// resolveIds gives every id-less block of `target` the id of the block of
// `reference` it corresponds to, found by aligning siblings on content: equal
// blocks pair up (longest common subsequence), and what remains between two
// anchors pairs positionally when the types match (a modified block). Whatever
// stays unpaired gets a fresh id (an added block). Returns false when the
// alignment would be too expensive.
//
// When none of target's ids occur in the reference, the ids are not
// meaningful (a writer that invents its own) and are ignored altogether.
function resolveIds(reference: Blk[], target: Blk[]): boolean {
	const refIds = new Set([...walk(reference)].map((b) => b.id));
	const ids = [...walk(target)].filter(hasId);
	if (ids.length > 0 && !ids.some((b) => refIds.has(b.id))) {
		for (const b of walk(target)) b.id = `${NO_ID}ignored:${b.id}`;
	}
	return align(reference, target);
}

function align(reference: Blk[], target: Blk[]): boolean {
	const byId = new Map(reference.map((b) => [b.id, b]));
	const claimed = new Set<string>();
	// Targets that carry an id keep it: one the reference knows is a match, one
	// it does not know is an added block.
	for (const t of target) if (hasId(t) && byId.has(t.id)) claimed.add(t.id);
	const pool = reference.filter((r) => !claimed.has(r.id));
	const todo = target.filter((t) => !hasId(t));
	if (pool.length * todo.length > MAX_ALIGN_CELLS) return false;

	// LCS over the unclaimed blocks by content.
	const n = pool.length;
	const m = todo.length;
	const rk = pool.map(ownKey);
	const tk = todo.map(ownKey);
	const lcs: number[][] = Array.from({ length: n + 1 }, () =>
		new Array<number>(m + 1).fill(0),
	);
	const at = (i: number, j: number): number => lcs[i]?.[j] ?? 0;
	for (let i = n - 1; i >= 0; i--) {
		for (let j = m - 1; j >= 0; j--) {
			(lcs[i] as number[])[j] =
				rk[i] === tk[j]
					? at(i + 1, j + 1) + 1
					: Math.max(at(i + 1, j), at(i, j + 1));
		}
	}
	const pairs: [number, number][] = [];
	for (let i = 0, j = 0; i < n && j < m; ) {
		if (rk[i] === tk[j]) {
			pairs.push([i, j]);
			i++;
			j++;
		} else if (at(i + 1, j) >= at(i, j + 1)) i++;
		else j++;
	}
	// Anchors split the rest into gaps; pair a gap's blocks positionally.
	const assign = (r: Blk | undefined, t: Blk | undefined) => {
		if (!r || !t) return;
		t.id = r.id;
		claimed.add(r.id);
	};
	let pi = 0;
	let pj = 0;
	for (const [ai, aj] of [...pairs, [n, m] as [number, number]]) {
		let i = pi;
		let j = pj;
		while (i < ai && j < aj) {
			if (pool[i]?.type === todo[j]?.type) assign(pool[i], todo[j]);
			i++;
			j++;
		}
		if (ai < n) assign(pool[ai], todo[aj]);
		pi = ai + 1;
		pj = aj + 1;
	}
	for (const t of todo) if (!hasId(t)) t.id = crypto.randomUUID();
	// Recurse into the children of every pair.
	const refById = new Map(reference.map((r) => [r.id, r]));
	for (const t of target) {
		const r = refById.get(t.id);
		if (!align(r?.children ?? [], t.children)) return false;
	}
	return true;
}

// ── Applying to the Y.Doc ────────────────────────────────────────────────────

const isEl = (x: unknown, name: string): x is Y.XmlElement =>
	x instanceof Y.XmlElement && x.nodeName.toLowerCase() === name;

function groupOf(fragment: Y.XmlFragment): Y.XmlElement | undefined {
	return fragment
		.toArray()
		.find((c): c is Y.XmlElement => isEl(c, "blockgroup"));
}

function containersOf(group: Y.XmlElement | Y.XmlFragment): Y.XmlElement[] {
	return group
		.toArray()
		.filter((c): c is Y.XmlElement => isEl(c, "blockcontainer"));
}

interface Found {
	el: Y.XmlElement;
	group: Y.XmlElement;
}

function findContainer(group: Y.XmlElement, id: string): Found | undefined {
	for (const el of containersOf(group)) {
		if (el.getAttribute("id") === id) return { el, group };
		const sub = el
			.toArray()
			.find((c): c is Y.XmlElement => isEl(c, "blockgroup"));
		const hit = sub && findContainer(sub, id);
		if (hit) return hit;
	}
	return undefined;
}

// containerFor builds the Y node of one block (with its children) in a scratch
// doc and returns a detached copy ready to insert into the room. With
// `contentOnly` the copy is the block's content node alone (no children), which
// is what replaces the content of a block that already sits in the room.
function containerFor(block: Blk, contentOnly = false): Y.XmlElement {
	const scratch = new Y.Doc();
	try {
		const frag = scratch.getXmlFragment(SCRATCH);
		blocksToYXmlFragment(editor(), [block] as never, frag);
		const g = groupOf(frag);
		const el = g && containersOf(g)[0];
		if (!el) throw new Error("block produced no container");
		if (!contentOnly) return el.clone();
		const content = el.get(0);
		if (!(content instanceof Y.XmlElement)) throw new Error("no content node");
		return content.clone();
	} finally {
		scratch.destroy();
	}
}

// mergeExternalChange folds a write made outside the editors into `doc`.
// Everything happens in one transaction; the caller broadcasts the diff.
export function mergeExternalChange(
	doc: Y.Doc,
	fragmentName: string,
	oldContent: unknown[],
	newContent: unknown[],
): MergeResult {
	let ours: Blk[];
	let base: Blk[];
	let theirs: Blk[];
	try {
		ours = yDocToBlocks(editor(), doc, fragmentName) as unknown as Blk[];
		base = canon(oldContent);
		theirs = canon(newContent);
	} catch {
		return { applied: 0, conflicts: 0, skipped: "unsupported" };
	}
	if (!resolveIds(ours, base) || !resolveIds(base, theirs)) {
		return { applied: 0, conflicts: 0, skipped: "too_large" };
	}

	const baseIdx = indexOf(base);
	const theirIdx = indexOf(theirs);
	const ourIdx = indexOf(ours);
	let applied = 0;
	let conflicts = 0;

	doc.transact(() => {
		const rootGroup = groupOf(doc.getXmlFragment(fragmentName));
		if (!rootGroup) return;

		// Added and changed blocks, in document order so an insert can anchor on
		// a previous sibling that was inserted a moment ago.
		const visit = (blocks: Blk[], parentId: string | null) => {
			blocks.forEach((b, i) => {
				const before = baseIdx.get(b.id);
				if (!before) {
					if (!ourIdx.has(b.id) && insertBlock(b, i, blocks, parentId)) {
						applied++;
						// The subtree came along with it.
						for (const d of walk(b.children))
							ourIdx.set(d.id, { block: d, parentId: b.id });
						ourIdx.set(b.id, { block: b, parentId });
						return;
					}
				} else if (ownKey(before.block) !== ownKey(b)) {
					const mine = ourIdx.get(b.id);
					if (!mine) return; // deleted live: keep it deleted
					if (ownKey(mine.block) !== ownKey(before.block)) {
						if (ownKey(mine.block) !== ownKey(b)) conflicts++;
						return;
					}
					if (replaceOwn(b)) applied++;
				}
				visit(b.children, b.id);
			});
		};

		const groupFor = (parentId: string | null): Y.XmlElement => {
			if (parentId === null) return rootGroup;
			const parent = findContainer(rootGroup, parentId);
			if (!parent) return rootGroup;
			let sub = parent.el
				.toArray()
				.find((c): c is Y.XmlElement => isEl(c, "blockgroup"));
			if (!sub) {
				sub = new Y.XmlElement("blockGroup");
				parent.el.insert(parent.el.length, [sub]);
			}
			return sub;
		};

		const insertBlock = (
			b: Blk,
			i: number,
			siblings: Blk[],
			parentId: string | null,
		): boolean => {
			try {
				const group = groupFor(parentId);
				let at = 0;
				for (let k = i - 1; k >= 0; k--) {
					const idx = containersOf(group).findIndex(
						(c) => c.getAttribute("id") === siblings[k]?.id,
					);
					if (idx >= 0) {
						at = idx + 1;
						break;
					}
				}
				group.insert(at, [containerFor(b)]);
				return true;
			} catch {
				return false;
			}
		};

		const replaceOwn = (b: Blk): boolean => {
			const found = findContainer(rootGroup, b.id);
			if (!found) return false;
			try {
				const content = containerFor({ ...b, children: [] }, true);
				found.el.delete(0, 1);
				found.el.insert(0, [content]);
				return true;
			} catch {
				return false;
			}
		};

		visit(theirs, null);

		// Removed blocks: only when the whole subtree is untouched. Deepest
		// first is unnecessary: deleting a container removes its descendants.
		const untouched = (b: Blk): boolean => {
			const original = baseIdx.get(b.id);
			if (!original || theirIdx.has(b.id)) return false;
			if (ownKey(original.block) !== ownKey(b)) return false;
			return b.children.every(untouched);
		};
		for (const [id, { block }] of baseIdx) {
			if (theirIdx.has(id)) continue;
			const mine = ourIdx.get(id);
			if (!mine) continue;
			// Skip descendants of a block removed together with it.
			let ancestor = baseIdx.get(id)?.parentId ?? null;
			let covered = false;
			while (ancestor !== null) {
				if (!theirIdx.has(ancestor)) {
					covered = true;
					break;
				}
				ancestor = baseIdx.get(ancestor)?.parentId ?? null;
			}
			if (covered) continue;
			if (!untouched(mine.block)) {
				if (ownKey(mine.block) !== ownKey(block)) conflicts++;
				continue;
			}
			const found = findContainer(rootGroup, id);
			if (!found) continue;
			const at = found.group.toArray().indexOf(found.el);
			if (at >= 0) {
				found.group.delete(at, 1);
				applied++;
			}
		}
	}, "external");

	return { applied, conflicts };
}
