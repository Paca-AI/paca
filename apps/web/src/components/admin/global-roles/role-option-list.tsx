import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import {
	RoleMultiSelect,
	type RoleMultiSelectProps,
	type RoleOption,
} from "@/components/admin/global-roles/role-select";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";

export type { RoleOption };

export type RoleOptionListProps<T extends RoleOption> = RoleMultiSelectProps<T>;

/**
 * Chooses roles from a searchable multi-select popover (see RoleMultiSelect).
 * A holder can have several roles; what they may do is everything the roles
 * allow, minus anything a role denies.
 */
export const RoleOptionList = RoleMultiSelect;

/** The part of a query result the boundary reads. */
interface RolesQuery<T> {
	data: T[] | undefined;
	isPending: boolean;
	isError: boolean;
	isRefetching: boolean;
	refetch: () => Promise<unknown>;
}

/**
 * Shows what a roles query is doing — placeholders while it loads, a retry
 * when it fails, a note when there are none — and hands the roles to
 * `children` once there are some.
 */
export function RolesQueryBoundary<T>({
	query,
	children,
}: {
	query: RolesQuery<T>;
	children: (roles: T[]) => ReactNode;
}) {
	const { t } = useTranslation("admin");

	if (query.isPending) {
		return (
			<div className="flex flex-col gap-2" aria-hidden="true">
				<Skeleton className="h-9 rounded-lg" />
			</div>
		);
	}

	if (query.isError) {
		return (
			<div
				role="alert"
				className="flex items-center justify-between gap-3 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive"
			>
				<span>{t("rolePicker.loadFailed")}</span>
				<Button
					variant="outline"
					size="sm"
					onClick={() => void query.refetch()}
					disabled={query.isRefetching}
				>
					{t("rolePicker.retry")}
				</Button>
			</div>
		);
	}

	if (!query.data || query.data.length === 0) {
		return (
			<p className="rounded-lg border border-dashed px-3 py-4 text-center text-sm text-muted-foreground">
				{t("rolePicker.empty")}
			</p>
		);
	}

	return <>{children(query.data)}</>;
}
