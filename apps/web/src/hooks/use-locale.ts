import { useEffect, useState } from "react";
import i18n, {
	LOCALE_LABELS,
	loadLanguage,
	resolveLocale,
	SUPPORTED_LANGUAGES,
	type SupportedLanguage,
} from "@/i18n";

export interface LocaleOption {
	code: SupportedLanguage;
	nativeLabel: string;
}

export const SUPPORTED_LOCALES: LocaleOption[] = SUPPORTED_LANGUAGES.map(
	(code) => ({ code, nativeLabel: LOCALE_LABELS[code] }),
);

export function useLocale() {
	const [locale, setLocale] = useState<SupportedLanguage>(() =>
		resolveLocale(i18n.language),
	);

	useEffect(() => {
		const onLanguageChanged = (language: string) => {
			setLocale(resolveLocale(language));
		};

		i18n.on("languageChanged", onLanguageChanged);
		return () => {
			i18n.off("languageChanged", onLanguageChanged);
		};
	}, []);

	function set(next: SupportedLanguage) {
		void loadLanguage(next).then(() => i18n.changeLanguage(next));
	}

	return { locale, set, supportedLocales: SUPPORTED_LOCALES };
}
