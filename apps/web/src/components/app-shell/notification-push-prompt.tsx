import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BellRing, X } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	currentPushSubscription,
	enablePush,
	isPushOptedOut,
	isPushSupported,
	pushPermission,
	pushSettingsQueryOptions,
	setPushOptedOut,
} from "@/lib/push-api";

const DISMISSED_KEY = "paca:push-prompt-dismissed";

function wasDismissed(): boolean {
	try {
		return window.localStorage.getItem(DISMISSED_KEY) === "true";
	} catch {
		return false;
	}
}

/** A one-line offer, in the bell, to get notified with Paca closed. It is
 *  shown only while it can work and has not been answered: the browser can ask,
 *  this one has no subscription, and the user has neither switched push off nor
 *  waved this away. Clicking is the user gesture Safari and Firefox need before
 *  they will show their permission prompt. */
export function NotificationPushPrompt() {
	const { t } = useTranslation("appShell");
	const queryClient = useQueryClient();
	const supported = isPushSupported();
	const settings = useQuery({
		...pushSettingsQueryOptions,
		enabled: supported,
		staleTime: 5 * 60_000,
	});
	const device = useQuery({
		queryKey: ["push", "device"],
		queryFn: async () => (await currentPushSubscription()) !== null,
		enabled: supported,
	});
	const [dismissed, setDismissed] = useState(wasDismissed);

	const enable = useMutation({
		mutationFn: async () => {
			const key = settings.data?.public_key;
			if (!key) throw new Error("push settings not loaded");
			setPushOptedOut(false);
			return enablePush(key);
		},
		onSettled: () => {
			void queryClient.invalidateQueries({ queryKey: ["push"] });
		},
	});

	if (
		!supported ||
		dismissed ||
		device.data !== false ||
		!settings.data ||
		isPushOptedOut() ||
		pushPermission() !== "default" ||
		enable.data === "enabled"
	) {
		return null;
	}

	const dismiss = () => {
		try {
			window.localStorage.setItem(DISMISSED_KEY, "true");
		} catch {
			// Not remembered in private mode; it will simply come back.
		}
		setDismissed(true);
	};

	return (
		<div className="flex items-center gap-2.5 border-t bg-muted/40 px-4 py-2.5">
			<BellRing
				className="size-4 shrink-0 text-muted-foreground"
				aria-hidden="true"
			/>
			<p className="min-w-0 flex-1 text-xs leading-snug text-muted-foreground">
				{t("notifications.push.prompt")}
			</p>
			<Button
				size="xs"
				disabled={enable.isPending}
				onClick={() => enable.mutate()}
			>
				{t("notifications.push.enable")}
			</Button>
			<button
				type="button"
				onClick={dismiss}
				aria-label={t("notifications.push.dismiss")}
				title={t("notifications.push.dismiss")}
				className="flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground outline-none hover:bg-background hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
			>
				<X className="size-3.5" />
			</button>
		</div>
	);
}
