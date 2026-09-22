/** Colors a project permission's badge by the area it belongs to. */
export function projectPermissionBadgeClass(key: string): string {
	const domain = key.split(".").slice(0, 2).join(".");
	if (domain === "projects") {
		return "bg-primary/10 text-primary border-primary/20 dark:bg-primary/20";
	}
	if (domain === "project.members") {
		return "bg-violet-50 text-violet-700 border-violet-200 dark:bg-violet-900/20 dark:text-violet-400 dark:border-violet-700/30";
	}
	if (domain === "project.roles") {
		return "bg-sky-50 text-sky-700 border-sky-200 dark:bg-sky-900/20 dark:text-sky-400 dark:border-sky-700/30";
	}
	if (domain === "tasks") {
		return "bg-amber-50 text-amber-700 border-amber-200 dark:bg-amber-900/20 dark:text-amber-400 dark:border-amber-700/30";
	}
	if (domain === "sprints") {
		return "bg-emerald-50 text-emerald-700 border-emerald-200 dark:bg-emerald-900/20 dark:text-emerald-400 dark:border-emerald-700/30";
	}
	if (domain === "docs") {
		return "bg-purple-50 text-purple-700 border-purple-200 dark:bg-purple-900/20 dark:text-purple-400 dark:border-purple-700/30";
	}
	if (domain === "agents") {
		return "bg-pink-50 text-pink-700 border-pink-200 dark:bg-pink-900/20 dark:text-pink-400 dark:border-pink-700/30";
	}
	return "bg-muted text-muted-foreground border-border";
}
