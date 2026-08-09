import { describe, expect, it } from "vitest";
import { SUPPORTED_LANGUAGES } from "./config";

// Same glob pattern config.ts uses to build i18next resources, so this test
// sees exactly the files that ship to the app.
const localeModules = import.meta.glob("./locales/*/*.json", {
	eager: true,
}) as Record<string, { default: Record<string, unknown> }>;

type Json = string | number | boolean | null | Json[] | { [key: string]: Json };

// i18next resolves pluralized keys (e.g. "count_one", "count_other") via
// Intl.PluralRules for the active locale, so which suffixes legitimately
// exist varies by language - e.g. Russian needs "_few"/"_many" in addition to
// "_one"/"_other". Strip a trailing CLDR plural suffix so those keys compare
// as their shared base ("count") instead of tripping a false mismatch.
const PLURAL_SUFFIX = /_(zero|one|two|few|many|other)$/;

// Flattens an object to a sorted, deduplicated list of dotted key paths, e.g.
// { a: { b: 1 }, c: 2 } -> ["a.b", "c"]. Arrays and primitives are leaves,
// so a key that changes from an object to a string (or vice versa) between
// locales shows up as a different path rather than being silently ignored.
function collectKeyPaths(value: Json, prefix = ""): string[] {
	if (value === null || typeof value !== "object" || Array.isArray(value)) {
		return [prefix.replace(PLURAL_SUFFIX, "")];
	}
	const paths = Object.keys(value).flatMap((key) =>
		collectKeyPaths(value[key], prefix ? `${prefix}.${key}` : key),
	);
	return [...new Set(paths)].sort();
}

function parseLocalePath(modulePath: string): {
	lang: string;
	namespace: string;
} {
	const match = modulePath.match(/\.\/locales\/([^/]+)\/([^/]+)\.json$/);
	if (!match) {
		throw new Error(`Unexpected locale module path: ${modulePath}`);
	}
	const [, lang, namespace] = match;
	return { lang, namespace };
}

const localesByLang: Record<string, Record<string, Json>> = {};
for (const [modulePath, mod] of Object.entries(localeModules)) {
	const { lang, namespace } = parseLocalePath(modulePath);
	localesByLang[lang] ??= {};
	localesByLang[lang][namespace] = mod.default as Json;
}

const baseLang = "en";
const namespaces = Object.keys(localesByLang[baseLang]).sort();
const otherLanguages = SUPPORTED_LANGUAGES.filter((lang) => lang !== baseLang);

describe("locale JSON structure", () => {
	it("has exactly one set of locale files per supported language, no more, no less", () => {
		expect(Object.keys(localesByLang).sort()).toEqual(
			[...SUPPORTED_LANGUAGES].sort(),
		);
	});

	for (const lang of otherLanguages) {
		describe(lang, () => {
			it(`has the same namespace files as "${baseLang}"`, () => {
				expect(Object.keys(localesByLang[lang]).sort()).toEqual(namespaces);
			});

			for (const namespace of namespaces) {
				it(`"${namespace}" has the same keys as "${baseLang}"`, () => {
					const baseKeys = collectKeyPaths(localesByLang[baseLang][namespace]);
					const langKeys = collectKeyPaths(localesByLang[lang][namespace]);
					expect(langKeys).toEqual(baseKeys);
				});
			}
		});
	}
});
