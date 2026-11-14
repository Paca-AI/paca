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
	if (s.toLowerCase().startsWith(".paca-")) s = `_${s}`;
	if (Buffer.byteLength(s) > MAX_SEGMENT_BYTES) {
		let out = "";
		for (const ch of s) {
			if (Buffer.byteLength(out + ch) > MAX_SEGMENT_BYTES) break;
			out += ch;
		}
		s = out;
	}
	return s;
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
 * separators: `<folder path>/<title>.md`. `taken` holds the lower-cased
 * paths already used (files and folders); a name already in it (compared
 * case-insensitively, for case-insensitive file systems) gets
 * `~<first 8 characters of the id>` before `.md`. The result is added to
 * `taken`.
 */
export function mirrorFileFor(
	doc: MirrorDoc,
	folders: Record<string, MirrorFolder>,
	taken: Set<string>,
): string {
	const candidates = mirrorCandidates(doc, folders);
	let file = candidates.find((c) => !taken.has(c.toLowerCase()));
	const last = candidates[candidates.length - 1].slice(0, -3);
	for (let n = 2; !file; n++) {
		const c = `${last}~${n}.md`;
		if (!taken.has(c.toLowerCase())) file = c;
	}
	taken.add(file.toLowerCase());
	return file;
}

/** The names `mirrorFileFor` tries for `doc`, in order: the plain name, then
 *  with the first 8 characters of the id, then with the whole id. */
export function mirrorCandidates(
	doc: MirrorDoc,
	folders: Record<string, MirrorFolder>,
): string[] {
	const dir = folderPathFor(doc.folder_id, folders);
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
