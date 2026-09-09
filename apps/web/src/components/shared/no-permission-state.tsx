import type { LucideIcon } from "lucide-react";
import { Lock } from "lucide-react";

/**
 * Shown in place of a list/section whose data request came back 403 —
 * distinct from a generic fetch failure (server/network trouble, worth
 * retrying) and from a genuinely empty list (nothing to show, not a rights
 * problem). Generalizes the amber "no permission" card already duplicated
 * per feature in admin/users/UsersStates.tsx and
 * admin/global-roles/GlobalRolesStates.tsx — same visual treatment, so a
 * caller doesn't need to hand-roll it again.
 */
export function NoPermissionState({
	icon: Icon = Lock,
	title,
	description,
}: {
	icon?: LucideIcon;
	title: string;
	description?: string;
}) {
	return (
		<div className="flex flex-col items-center gap-3 rounded-xl border border-amber-200 bg-amber-50 py-14 text-center dark:border-amber-900/40 dark:bg-amber-900/10">
			<Icon className="size-8 text-amber-400 dark:text-amber-500" />
			<div className="max-w-sm px-4">
				<p className="text-sm font-medium text-amber-700 dark:text-amber-400">
					{title}
				</p>
				{description && (
					<p className="mt-0.5 text-xs text-muted-foreground">{description}</p>
				)}
			</div>
		</div>
	);
}
