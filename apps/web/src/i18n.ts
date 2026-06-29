import i18n from "i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import { initReactI18next } from "react-i18next";

import en from "./locales/en/translation.json";
import ko from "./locales/ko/translation.json";

export const SUPPORTED_LANGUAGES = ["en", "ko"] as const;
export type SupportedLanguage = (typeof SUPPORTED_LANGUAGES)[number];

i18n
	.use(LanguageDetector)
	.use(initReactI18next)
	.init({
		resources: {
			en: { translation: en },
			ko: { translation: ko },
		},
		fallbackLng: "en",
		supportedLngs: SUPPORTED_LANGUAGES as unknown as string[],
		nonExplicitSupportedLngs: true,
		interpolation: { escapeValue: false },
		detection: {
			order: ["localStorage", "navigator", "htmlTag"],
			caches: ["localStorage"],
			lookupLocalStorage: "paca:lang",
		},
	});

// Keep <html lang> in sync for accessibility / SEO.
const applyHtmlLang = (lng: string) => {
	document.documentElement.lang = lng;
};
i18n.on("languageChanged", applyHtmlLang);
applyHtmlLang(i18n.resolvedLanguage ?? "en");

export default i18n;
