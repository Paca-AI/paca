import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowUpRight, TriangleAlert, X } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { usePermissions } from "@/hooks/use-permissions";
import { pluginsQueryOptions } from "@/lib/plugin-api";

const DISMISS_KEY = "paca-legacy-plugins-banner-dismissed";

function readDismissed(): string | null {
	try {
		return window.localStorage.getItem(DISMISS_KEY);
	} catch {
		return null;
	}
}

/**
 * Home banner for administrators: lists installed plugins whose package still
 * uses the retired requirePermissions middleware. Their stored manifest was
 * converted when Paca was upgraded, but reinstalling or upgrading that build is
 * rejected until the plugin's author ships one that uses requireActions.
 * Dismissible per set of plugins (a newly affected plugin re-shows it).
 */
export function LegacyPluginsBanner() {
	const { t } = useTranslation("shared");
	const { hasPermission } = usePermissions();
	const canManage = hasPermission("plugins:write");
	const { data: plugins } = useQuery({
		...pluginsQueryOptions,
		enabled: canManage,
	});
	const [dismissed, setDismissed] = useState<string | null>(readDismissed);

	const affected = (plugins ?? []).filter((p) => p.legacy_permissions);
	if (!canManage || affected.length === 0) {
		return null;
	}

	const signature = affected
		.map((p) => p.name)
		.sort()
		.join(",");
	if (dismissed === signature) {
		return null;
	}

	const dismiss = () => {
		try {
			window.localStorage.setItem(DISMISS_KEY, signature);
		} catch {
			// ignore storage failures — dismissal just won't persist
		}
		setDismissed(signature);
	};

	const names = affected
		.map((p) => p.manifest.displayName || p.name)
		.join(", ");

	return (
		<div
			role="alert"
			className="flex items-start gap-3 border-b border-amber-300/40 bg-amber-50 px-6 py-3 dark:border-amber-700/30 dark:bg-amber-900/15"
		>
			<TriangleAlert className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
			<div className="flex flex-1 flex-col gap-1 text-sm">
				<span className="font-semibold text-foreground">
					{t("home.legacyPluginsBanner.title", { count: affected.length })}
				</span>
				<span className="text-muted-foreground">
					{t("home.legacyPluginsBanner.description", {
						count: affected.length,
						names,
					})}
				</span>
				<Link
					to="/admin/plugins"
					className="inline-flex w-fit items-center gap-0.5 font-medium text-primary hover:underline"
				>
					{t("home.legacyPluginsBanner.manage")}
					<ArrowUpRight className="size-3" />
				</Link>
			</div>
			<button
				type="button"
				onClick={dismiss}
				aria-label={t("home.legacyPluginsBanner.dismiss")}
				className="shrink-0 rounded p-1 text-muted-foreground transition-colors hover:bg-amber-200/40 hover:text-foreground dark:hover:bg-amber-800/30"
			>
				<X className="size-4" />
			</button>
		</div>
	);
}
