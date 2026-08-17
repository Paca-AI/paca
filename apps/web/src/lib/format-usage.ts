import i18n from "@/i18n";

export function formatCompactTokens(count: number): string {
	return new Intl.NumberFormat(i18n.language, {
		notation: "compact",
		maximumFractionDigits: 1,
	}).format(count);
}

/**
 * Most per-conversation costs round cleanly to cents, but a short chat turn
 * can cost a fraction of a cent — the default 2-decimal formatter would
 * render that as a misleading "$0.00" (money was spent, but shows as free),
 * so fall back to 4 decimals below one cent.
 */
export function formatUsageCost(amountUSD: number): string {
	if (amountUSD > 0 && amountUSD < 0.01) {
		return `$${amountUSD.toFixed(4)}`;
	}
	return new Intl.NumberFormat(i18n.language, {
		style: "currency",
		currency: "USD",
	}).format(amountUSD);
}
