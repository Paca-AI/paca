export function formatDate(iso: string) {
	return new Date(iso).toLocaleDateString("en-US", {
		year: "numeric",
		month: "short",
		day: "numeric",
	});
}

export function permissionBadgeClass(key: string): string {
	const domain = key.split(".")[0];
	if (domain === "global_roles") {
		return "bg-primary/10 text-primary border-primary/20 dark:bg-primary/20";
	}
	if (domain === "users") {
		return "bg-amber-50 text-amber-700 border-amber-200 dark:bg-amber-900/20 dark:text-amber-400 dark:border-amber-700/30";
	}
	return "bg-muted text-muted-foreground border-border";
}

export function activePermissions(perms: Record<string, boolean>) {
	return Object.entries(perms)
		.filter(([, value]) => value)
		.map(([key]) => key);
}
