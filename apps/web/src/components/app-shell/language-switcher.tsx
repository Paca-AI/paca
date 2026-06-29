import { useTranslation } from "react-i18next";

import { cn } from "@/lib/utils";

const LANGUAGES = [
	{ code: "en", label: "EN" },
	{ code: "ko", label: "한국어" },
] as const;

/** Segmented EN / 한국어 language switcher, styled to match the theme switcher. */
export function LanguageSwitcher() {
	const { i18n, t } = useTranslation();
	const current = i18n.resolvedLanguage;

	return (
		<>
			{/* Collapsed: cycle between languages with a compact button */}
			<SidebarCollapsedSwitcher current={current} />

			{/* Expanded: segmented control */}
			<div className="flex items-center justify-between px-2 py-1.5 group-data-[collapsible=icon]:hidden">
				<span className="text-xs font-medium text-sidebar-foreground/50 tracking-wide">
					{t("common.language")}
				</span>
				<div className="flex items-center gap-0.5 rounded-md border border-sidebar-border bg-sidebar p-0.5">
					{LANGUAGES.map((lang) => (
						<button
							key={lang.code}
							type="button"
							onClick={() => i18n.changeLanguage(lang.code)}
							title={lang.label}
							className={cn(
								"flex h-6 items-center justify-center rounded px-2 text-xs transition-all duration-150",
								current === lang.code
									? "bg-sidebar-accent text-sidebar-accent-foreground shadow-sm"
									: "text-sidebar-foreground/40 hover:text-sidebar-foreground/70",
							)}
						>
							{lang.label}
						</button>
					))}
				</div>
			</div>
		</>
	);
}

function SidebarCollapsedSwitcher({ current }: { current?: string }) {
	const { i18n } = useTranslation();
	const next = current === "ko" ? "en" : "ko";
	const shown = current === "ko" ? "한" : "EN";

	return (
		<div className="hidden justify-center py-1 group-data-[collapsible=icon]:flex">
			<button
				type="button"
				onClick={() => i18n.changeLanguage(next)}
				title={`Language: ${current} — click to switch`}
				className="flex size-7 items-center justify-center rounded-md text-xs font-semibold text-sidebar-foreground/50 hover:bg-sidebar-accent hover:text-sidebar-foreground transition-all duration-150"
			>
				{shown}
			</button>
		</div>
	);
}
