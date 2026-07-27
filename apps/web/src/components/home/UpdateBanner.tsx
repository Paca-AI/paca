import { useQuery } from "@tanstack/react-query";
import { ArrowUpRight, Sparkles, X } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { type ReleaseInfo, versionQueryOptions } from "@/lib/version-api";

const DISMISS_KEY = "paca-update-banner-dismissed";

function readDismissed(): string | null {
	try {
		return window.localStorage.getItem(DISMISS_KEY);
	} catch {
		return null;
	}
}

/**
 * Home banner that surfaces newer releases of the upstream project and/or the
 * fork. Rendered only when the API reports an available update; dismissible per
 * release set (a new release re-shows it).
 */
export function UpdateBanner() {
	const { t } = useTranslation("shared");
	const { data } = useQuery(versionQueryOptions());
	const [dismissed, setDismissed] = useState<string | null>(readDismissed);

	const updates: { kind: "upstream" | "fork"; info: ReleaseInfo }[] = [];
	if (data?.upstream?.hasUpdate) {
		updates.push({ kind: "upstream", info: data.upstream });
	}
	if (data?.fork?.hasUpdate) {
		updates.push({ kind: "fork", info: data.fork });
	}

	if (updates.length === 0) {
		return null;
	}

	// Encode the set of latest versions so dismissing hides only this exact set;
	// a later release produces a new signature and shows the banner again.
	const signature = updates.map((u) => u.info.latest).join(",");
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

	return (
		<div className="flex items-start gap-3 border-b border-primary/20 bg-primary/10 px-6 py-3">
			<Sparkles className="mt-0.5 size-4 shrink-0 text-primary" />
			<div className="flex flex-1 flex-col gap-1 text-sm">
				<div className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
					<span className="font-semibold text-foreground">
						{t("home.updateBanner.title")}
					</span>
					<span className="text-xs text-muted-foreground">
						{t("home.updateBanner.current", { version: data?.current })}
					</span>
				</div>
				<div className="flex flex-col gap-1 sm:flex-row sm:flex-wrap sm:items-center sm:gap-x-5">
					{updates.map((u) => (
						<a
							key={u.kind}
							href={u.info.url}
							target="_blank"
							rel="noopener noreferrer"
							className="inline-flex items-center gap-1.5 text-muted-foreground transition-colors hover:text-foreground"
						>
							<span>
								{t(`home.updateBanner.${u.kind}`, { version: u.info.latest })}
							</span>
							<span className="inline-flex items-center gap-0.5 font-medium text-primary">
								{t("home.updateBanner.viewRelease")}
								<ArrowUpRight className="size-3" />
							</span>
						</a>
					))}
				</div>
			</div>
			<button
				type="button"
				onClick={dismiss}
				aria-label={t("home.updateBanner.dismiss")}
				className="shrink-0 rounded p-1 text-muted-foreground transition-colors hover:bg-primary/15 hover:text-foreground"
			>
				<X className="size-4" />
			</button>
		</div>
	);
}
