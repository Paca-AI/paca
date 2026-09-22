import { ShieldAlert } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
	activePermissions,
	permissionBadgeClass,
} from "@/components/admin/global-roles/utils";

interface PermissionSummaryProps {
	role: { permissions: Record<string, unknown> };
	/** Show this many permissions, then fold the rest into "+N". Omit to show all. */
	limit?: number;
	/** Colors a permission's badge. Global permissions by default. */
	badgeClass?: (permission: string) => string;
}

/**
 * A glance at what a global role grants: "Full access" for the `*` wildcard,
 * otherwise its permissions as badges (already de-duplicated, so a wildcard
 * stands in for the keys it covers).
 */
export function PermissionSummary({
	role,
	limit,
	badgeClass = permissionBadgeClass,
}: PermissionSummaryProps) {
	const { t } = useTranslation("admin");
	const permissions = activePermissions(role.permissions);

	if (permissions.includes("*")) {
		return (
			<span className="inline-flex w-fit items-center gap-1 rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-xs font-medium leading-none text-amber-700 dark:border-amber-700/30 dark:bg-amber-900/20 dark:text-amber-400">
				<ShieldAlert className="size-3" aria-hidden="true" />
				{t("rolePicker.fullAccess")}
			</span>
		);
	}

	if (permissions.length === 0) {
		return (
			<span className="text-xs italic text-muted-foreground/60">
				{t("globalRoles.table.noPermissionsAssigned")}
			</span>
		);
	}

	const shown = limit === undefined ? permissions : permissions.slice(0, limit);
	const hidden = permissions.slice(shown.length);
	return (
		<div className="flex flex-wrap items-center gap-1">
			{shown.map((permission) => (
				<span
					key={permission}
					className={`inline-flex items-center rounded-full border px-2 py-0.5 font-mono text-xs font-medium leading-none ${badgeClass(permission)}`}
				>
					{permission}
				</span>
			))}
			{hidden.length > 0 ? (
				<span
					className="px-1 text-xs text-muted-foreground"
					title={hidden.join(", ")}
				>
					+{hidden.length}
				</span>
			) : null}
		</div>
	);
}
