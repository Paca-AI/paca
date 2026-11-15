import path from "node:path";

/** A folder as the pull state keeps it. */
export interface MirrorFolder {
	parent_id: string | null;
	name: string;
}

/** What naming a mirror file needs from a document. */
export interface MirrorDoc {
	id: string;
	title: string;
	folder_id: string | null;
}

/** Longest segment kept, in UTF-8 bytes. Leaves room for "~<8>.md" under
 *  the usual 255-byte file name limit. */
const MAX_SEGMENT_BYTES = 200;

/**
 * Turns one untrusted name (a document title or folder name from the server)
 * into a safe path segment: path separators, characters Windows forbids and
 * control characters become `_`; `.`, `..`, empty and blank names become `_`;
 * a name starting with `.paca-` (the mirror's own files) gets a `_` prefix;
 * long names are cut.
 */
export function sanitizeSegment(name: string): string {
	// biome-ignore lint/suspicious/noControlCharactersInRegex: control characters are what this removes
	let s = name.replace(/[/\\:*?"<>|\u0000-\u001f\u007f]/g, "_");
	if (s.trim() === "" || s === "." || s === "..") s = "_";
	if (Buffer.byteLength(s) > MAX_SEGMENT_BYTES) {
		let out = "";
		for (const ch of s) {
			if (Buffer.byteLength(out + ch) > MAX_SEGMENT_BYTES) break;
			out += ch;
		}
		s = out;
	}
	// Windows drops trailing dots and spaces, and reserves device names.
	s = s.replace(/[. ]+$/, (m) => "_".repeat(m.length));
	if (/^(con|prn|aux|nul|com[1-9]|lpt[1-9])(\.|$)/i.test(s)) s = `_${s}`;
	if (s.toLowerCase().startsWith(".paca-")) s = `_${s}`;
	return s;
}

/** The key two names are compared by: the same file on a case-insensitive,
 *  normalization-insensitive file system (APFS, NTFS) has the same key. */
export function nameKey(name: string): string {
	return name.normalize("NFC").toLowerCase();
}

/**
 * The mirror folder of every folder in `folders` ("A/B"). No two folders
 * get directories with the same name key, and none gets a directory whose
 * path is taken by a file (`files` holds name keys of files in the mirror).
 * Among folders that would clash, a folder keeps its directory from the
 * last pull (`prev`) while that is still one of its names; the others are
 * named by lowest id first, the first taking the plain name and the rest
 * `~<first 8 characters of the id>` (then the whole id, then `~2`, ...),
 * as `mirrorFileFor` names documents. A suffix is kept after the clash
 * ends, so files do not move back and forth.
 */
export function folderDirs(
	folders: Record<string, MirrorFolder>,
	files: Set<string>,
	prev: Record<string, string> = {},
): Record<string, string> {
	const out: Record<string, string> = {};
	const used = new Set(files);
	const ids = Object.keys(folders).sort();
	const childrenOf = new Map<string, string[]>();
	const top: string[] = [];
	for (const id of ids) {
		const parent = folders[id].parent_id;
		if (parent && parent in folders) {
			const list = childrenOf.get(parent) ?? [];
			list.push(id);
			childrenOf.set(parent, list);
		} else top.push(id);
	}

	/** Names `group` (sibling folders under `parentDir`), then their
	 *  children. */
	const assign = (group: string[], parentDir: string) => {
		const join = (seg: string) => (parentDir ? `${parentDir}/${seg}` : seg);
		const candidates = (id: string) => {
			const base = sanitizeSegment(folders[id].name);
			const idPart = sanitizeSegment(id);
			return [base, `${base}~${idPart.slice(0, 8)}`, `${base}~${idPart}`].map(
				join,
			);
		};
		const isName = (id: string, dir: string) => {
			const c = candidates(id);
			if (c.includes(dir)) return true;
			const rest = dir.startsWith(`${c[2]}~`) ? dir.slice(c[2].length + 1) : "";
			return /^[0-9]+$/.test(rest);
		};
		const todo = group.filter((id) => !(id in out));
		for (const id of todo) {
			const p = prev[id];
			if (typeof p === "string" && isName(id, p) && !used.has(nameKey(p))) {
				out[id] = p;
				used.add(nameKey(p));
			}
		}
		for (const id of todo) {
			if (id in out) continue;
			const c = candidates(id);
			let dir = c.find((d) => !used.has(nameKey(d)));
			for (let n = 2; !dir; n++)
				if (!used.has(nameKey(`${c[2]}~${n}`))) dir = `${c[2]}~${n}`;
			out[id] = dir;
			used.add(nameKey(dir));
		}
		for (const id of todo) {
			const kids = childrenOf.get(id);
			if (kids) assign(kids, out[id]);
		}
	};
	assign(top, "");
	// Folders in a parent cycle are not reached from the top: start each
	// remaining cycle at the top level.
	for (const id of ids) if (!(id in out)) assign([id], "");
	return out;
}

/** The sanitized folder path of `folderId` ("A/B"), or "" at the top level.
 *  A missing parent ends the chain; a cycle is cut where it repeats. */
export function folderPathFor(
	folderId: string | null,
	folders: Record<string, MirrorFolder>,
): string {
	const parts: string[] = [];
	const seen = new Set<string>();
	let id = folderId;
	while (id && !seen.has(id)) {
		seen.add(id);
		const f = folders[id];
		if (!f) break;
		parts.unshift(sanitizeSegment(f.name));
		id = f.parent_id;
	}
	return parts.join("/");
}

/**
 * The mirror file of `doc`, relative to the mirror folder, with `/`
 * separators: `<folder path>/<title>.md`. `taken` holds the name keys
 * (`nameKey`) of paths already used (files and folders); a name already in
 * it gets
 * `~<first 8 characters of the id>` before `.md`. The result is added to
 * `taken`. `dirs` (from `folderDirs`) overrides folder paths.
 */
export function mirrorFileFor(
	doc: MirrorDoc,
	folders: Record<string, MirrorFolder>,
	taken: Set<string>,
	dirs?: Record<string, string>,
): string {
	const candidates = mirrorCandidates(doc, folders, dirs);
	let file = candidates.find((c) => !taken.has(nameKey(c)));
	const last = candidates[candidates.length - 1].slice(0, -3);
	for (let n = 2; !file; n++) {
		const c = `${last}~${n}.md`;
		if (!taken.has(nameKey(c))) file = c;
	}
	taken.add(nameKey(file));
	return file;
}

/** The names `mirrorFileFor` tries for `doc`, in order: the plain name, then
 *  with the first 8 characters of the id, then with the whole id. */
export function mirrorCandidates(
	doc: MirrorDoc,
	folders: Record<string, MirrorFolder>,
	dirs?: Record<string, string>,
): string[] {
	const dir =
		(doc.folder_id && dirs?.[doc.folder_id]) ||
		folderPathFor(doc.folder_id, folders);
	const base = sanitizeSegment(doc.title);
	const join = (name: string) => (dir ? `${dir}/${name}` : name);
	const idPart = sanitizeSegment(doc.id);
	return [
		join(`${base}.md`),
		join(`${base}~${idPart.slice(0, 8)}.md`),
		join(`${base}~${idPart}.md`),
	];
}

/** Whether `target` is strictly inside `dir` once both are resolved. */
export function isInside(dir: string, target: string): boolean {
	const root = path.resolve(dir);
	const resolved = path.resolve(target);
	return resolved.startsWith(root + path.sep);
}
