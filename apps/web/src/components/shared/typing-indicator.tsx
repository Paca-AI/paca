import { useTranslation } from "react-i18next";

/** "Mina is typing…" under a composer. Reserves its line so the layout does
 *  not jump when somebody starts or stops typing. */
export function TypingIndicator({ names }: { names?: string[] }) {
	const { t } = useTranslation("shared");
	const list = names ?? [];
	let text = "";
	if (list.length === 1) {
		text = t("activityPane.typingOne", { name: list[0] });
	} else if (list.length === 2) {
		text = t("activityPane.typingTwo", { a: list[0], b: list[1] });
	} else if (list.length > 2) {
		text = t("activityPane.typingMany", {
			name: list[0],
			n: list.length - 1,
		});
	}
	return (
		<p
			role="status"
			aria-live="polite"
			data-testid="typing-indicator"
			className="h-5 shrink-0 truncate px-4 text-xs leading-5 text-muted-foreground/90"
		>
			{text}
		</p>
	);
}
