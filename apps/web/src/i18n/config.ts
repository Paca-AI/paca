import i18next from "i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import { initReactI18next } from "react-i18next";
import { onStorageKeyChange } from "@/lib/storage-sync";

export const SUPPORTED_LANGUAGES = [
	"en",
	"vi",
	"ko",
	"zh-CN",
	"ja",
	"es",
	"fr",
	"ru",
	"pt-BR",
] as const;

export type SupportedLanguage = (typeof SUPPORTED_LANGUAGES)[number];

export const LOCALE_LABELS: Record<SupportedLanguage, string> = {
	en: "English",
	vi: "Tiếng Việt",
	ko: "한국어",
	"zh-CN": "简体中文",
	ja: "日本語",
	es: "Español",
	fr: "Français",
	ru: "Русский",
	"pt-BR": "Português (Brasil)",
};

export const LOCALE_STORAGE_KEY = "locale";

// "en" ships eagerly — it's the fallbackLng, so it must always be available
// synchronously. Every other language is fetched on demand via loadLanguage()
// below. Eagerly globbing all nine locales here used to bundle ~1.2MB of
// translation JSON into main.tsx's entry chunk on every page load, regardless
// of which single language a given session actually uses.
const enModules = import.meta.glob("./locales/en/*.json", {
	eager: true,
}) as Record<string, { default: Record<string, unknown> }>;

const otherLocaleLoaders = import.meta.glob("./locales/*/*.json") as Record<
	string,
	() => Promise<{ default: Record<string, unknown> }>
>;

function namespaceFromPath(path: string): string {
	return path.match(/\/([^/]+)\.json$/)?.[1] ?? "";
}

const enResources: Record<string, Record<string, unknown>> = {};
for (const [path, mod] of Object.entries(enModules)) {
	enResources[namespaceFromPath(path)] = mod.default;
}

export function resolveLocale(language: string): SupportedLanguage {
	const exact = SUPPORTED_LANGUAGES.find((code) => code === language);
	if (exact) return exact;

	const base = language.split("-")[0];
	const baseMatch = SUPPORTED_LANGUAGES.find((code) => code === base);
	return baseMatch ?? "en";
}

const loadedLanguages = new Set<SupportedLanguage>(["en"]);

/**
 * Fetches and registers a language's namespace bundles, once per language.
 * "en" is already loaded eagerly (see enResources above), so this is a no-op
 * for it.
 */
export async function loadLanguage(lang: SupportedLanguage): Promise<void> {
	if (loadedLanguages.has(lang)) return;

	const entries = Object.entries(otherLocaleLoaders).filter(([path]) =>
		path.startsWith(`./locales/${lang}/`),
	);
	const modules = await Promise.all(
		entries.map(async ([path, load]) => {
			const mod = await load();
			return [namespaceFromPath(path), mod.default] as const;
		}),
	);
	for (const [namespace, data] of modules) {
		i18next.addResourceBundle(lang, namespace, data, true, true);
	}
	loadedLanguages.add(lang);
}

function detectInitialLanguage(): SupportedLanguage {
	if (typeof window === "undefined") return "en";
	const stored = window.localStorage.getItem(LOCALE_STORAGE_KEY);
	if (stored) return resolveLocale(stored);
	return resolveLocale(window.navigator.language);
}

const initialLanguage = detectInitialLanguage();

i18next
	.use(LanguageDetector)
	.use(initReactI18next)
	.init({
		resources: { en: enResources },
		lng: initialLanguage,
		fallbackLng: "en",
		supportedLngs: SUPPORTED_LANGUAGES,
		defaultNS: "common",
		interpolation: {
			escapeValue: false,
		},
		detection: {
			order: ["localStorage", "navigator"],
			lookupLocalStorage: LOCALE_STORAGE_KEY,
			caches: ["localStorage"],
		},
	});

// Resolves once the *initial* language's resources are ready. "en" resolves
// immediately since it's bundled eagerly; any other language waits on one
// dynamic import. main.tsx awaits this before its first render so components
// never flash untranslated/raw keys.
export const i18nReady: Promise<void> =
	initialLanguage === "en" ? Promise.resolve() : loadLanguage(initialLanguage);

if (typeof window !== "undefined") {
	onStorageKeyChange(LOCALE_STORAGE_KEY, (event) => {
		if (event.newValue && event.newValue !== i18next.language) {
			const next = resolveLocale(event.newValue);
			void loadLanguage(next).then(() => i18next.changeLanguage(next));
		}
	});

	const syncHtmlLang = (language: string) => {
		document.documentElement.lang = language;
	};
	syncHtmlLang(i18next.language);
	i18next.on("languageChanged", syncHtmlLang);
}

export default i18next;
