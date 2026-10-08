/** Up to two capital letters from a display name, for an avatar fallback. */
export function getInitials(name: string): string {
	return name
		.split(" ")
		.filter(Boolean)
		.map((n) => n[0])
		.join("")
		.toUpperCase()
		.slice(0, 2);
}
