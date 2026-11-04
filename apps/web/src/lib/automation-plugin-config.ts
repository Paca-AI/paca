import type {
	PluginNodeConfigSchema,
	PluginNodeType,
} from "@/lib/automation-api";

// Core-owned Git node types arrive in the same lists as plugin ones; their
// type is namespaced "git.*" and their label is prefixed "Git: ".
const GIT_NODE_TYPE_PREFIX = "git.";
const GIT_LABEL_PREFIX = /^Git:\s*/;

export function isGitNodeType(n: Pick<PluginNodeType, "type">): boolean {
	return n.type.startsWith(GIT_NODE_TYPE_PREFIX);
}

/** The label with the redundant "Git: " prefix dropped, for use inside the
 * palette's Git section. */
export function gitNodeShortLabel(label: string): string {
	return label.replace(GIT_LABEL_PREFIX, "");
}

export interface PluginConfigRow {
	title: string;
	value: string;
	isEnum: boolean;
}

function formatValue(raw: unknown): string {
	if (Array.isArray(raw)) return raw.map(formatValue).join(", ");
	if (typeof raw === "object" && raw !== null) return JSON.stringify(raw);
	return String(raw).replace(/\s+/g, " ").trim();
}

// "ready_for_review" -> "Ready for review"
function formatEnumValue(value: string): string {
	const spaced = value.replace(/[_-]+/g, " ").trim();
	return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

/**
 * The configured values of a plugin/Git node read through its configSchema —
 * undefined for a node that declares no config fields. Schema defaults are
 * only filled in when `useDefaults` is set, so an unconfigured node can be
 * told apart from a configured one.
 */
export function pluginConfigRows(
	schema: PluginNodeConfigSchema | undefined,
	nodeConfig: unknown,
	{ useDefaults = true }: { useDefaults?: boolean } = {},
): PluginConfigRow[] | undefined {
	const props = schema?.properties;
	if (!props || Object.keys(props).length === 0) return undefined;
	const config = (nodeConfig ?? {}) as Record<string, unknown>;
	const rows: PluginConfigRow[] = [];
	for (const [key, prop] of Object.entries(props)) {
		const raw = config[key] ?? (useDefaults ? prop.default : undefined);
		if (raw === undefined || raw === null || raw === "") continue;
		const value = formatValue(raw);
		if (value === "") continue;
		rows.push({
			title: prop.title ?? key,
			value: prop.enum ? formatEnumValue(value) : value,
			isEnum: !!prop.enum,
		});
	}
	return rows;
}

/**
 * The label for a plugin condition's single "match" output: the expected
 * value itself when there is exactly one enum field ("Merged"), otherwise
 * "Title: value" pairs. Undefined when nothing is configured.
 */
export function pluginMatchLabel(
	rows: PluginConfigRow[] | undefined,
	formatField: (row: PluginConfigRow) => string,
): string | undefined {
	if (!rows || rows.length === 0) return undefined;
	return rows
		.map((r) => (rows.length === 1 && r.isEnum ? r.value : formatField(r)))
		.join(", ");
}
