import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { InlineNotice } from "@/components/shared/inline-notice";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import type { NotificationType } from "@/lib/notification-api";
import {
	currentPushSubscription,
	disablePush,
	enablePush,
	isPushOptedOut,
	isPushSupported,
	type PushSettings,
	pushPermission,
	pushSettingsQueryOptions,
	setPushOptedOut,
	setPushPreference,
} from "@/lib/push-api";

const TYPE_ORDER: NotificationType[] = [
	"assigned",
	"mentioned",
	"question",
	"automation",
];

const deviceQueryKey = ["push", "device"] as const;

/** Web Push for the signed-in user: on for this browser by default (until they
 * switch it off here), and a choice of which kinds of notification are pushed.
 * Email opt-out lives in the SMTP plugin's own settings, below. */
export function PushNotificationsCard() {
	const { t } = useTranslation("profile");
	const queryClient = useQueryClient();
	const supported = isPushSupported();
	const settings = useQuery(pushSettingsQueryOptions);
	const device = useQuery({
		queryKey: deviceQueryKey,
		queryFn: async () => (await currentPushSubscription()) !== null,
		enabled: supported,
	});
	const [denied, setDenied] = useState(
		() => supported && pushPermission() === "denied",
	);

	const toggleDevice = useMutation({
		mutationFn: async ({ on, auto }: { on: boolean; auto?: boolean }) => {
			if (!on) {
				setPushOptedOut(true);
				await disablePush();
				return "disabled" as const;
			}
			const key = settings.data?.public_key;
			if (!key) throw new Error("push settings not loaded");
			if (!auto) setPushOptedOut(false);
			try {
				return await enablePush(key);
			} catch (error) {
				// The automatic attempt happens without a click, which some
				// browsers refuse to prompt for: that is not worth an error banner.
				if (auto) return "dismissed" as const;
				throw error;
			}
		},
		onSuccess: (result) => {
			setDenied(result === "denied");
			void queryClient.invalidateQueries({ queryKey: deviceQueryKey });
			void queryClient.invalidateQueries({
				queryKey: pushSettingsQueryOptions.queryKey,
			});
		},
	});

	// Push is on by default: once the card knows this browser's state and the
	// user never switched it off here, subscribe it. A browser that already holds
	// a subscription is re-registered too, as an idempotent upsert: the server
	// may have dropped its row (it deletes one whose key stopped matching), and
	// the account-wide subscription count cannot say whether *this* browser is
	// registered. This runs once per visit, and only when permission has not
	// been refused.
	const triedAuto = useRef(false);
	const { mutate: toggle } = toggleDevice;
	useEffect(() => {
		if (triedAuto.current) return;
		if (!supported || !settings.data || device.data === undefined) return;
		triedAuto.current = true;
		if (isPushOptedOut() || pushPermission() === "denied") return;
		toggle({ on: true, auto: true });
	}, [supported, settings.data, device.data, toggle]);

	const setPreference = useMutation({
		mutationFn: ({
			type,
			enabled,
		}: {
			type: NotificationType;
			enabled: boolean;
		}) => setPushPreference(type, enabled),
		onMutate: async ({ type, enabled }) => {
			const key = pushSettingsQueryOptions.queryKey;
			await queryClient.cancelQueries({ queryKey: key });
			const previous = queryClient.getQueryData<PushSettings>(key);
			if (previous) {
				queryClient.setQueryData<PushSettings>(key, {
					...previous,
					preferences: previous.preferences.map((p) =>
						p.type === type ? { ...p, enabled } : p,
					),
				});
			}
			return { previous };
		},
		onError: (_err, _vars, context) => {
			if (context?.previous) {
				queryClient.setQueryData(
					pushSettingsQueryOptions.queryKey,
					context.previous,
				);
			}
		},
		onSettled: () =>
			queryClient.invalidateQueries({
				queryKey: pushSettingsQueryOptions.queryKey,
			}),
	});

	const preferences = new Map(
		settings.data?.preferences.map((p) => [p.type, p.enabled]) ?? [],
	);

	return (
		<Card>
			<CardHeader>
				<CardTitle>{t("preferences.push.title")}</CardTitle>
				<CardDescription>{t("preferences.push.description")}</CardDescription>
			</CardHeader>
			<CardContent className="flex flex-col gap-5">
				{!supported ? (
					<InlineNotice tone="info">
						{t("preferences.push.unsupported")}
					</InlineNotice>
				) : settings.isPending ? (
					<Skeleton className="h-12 w-full" />
				) : settings.isError ? (
					<InlineNotice tone="error">
						{t("preferences.push.errors.load")}
					</InlineNotice>
				) : (
					<>
						<div className="flex items-center justify-between gap-4">
							<div>
								<p className="text-sm font-medium">
									{t("preferences.push.device.label")}
								</p>
								<p className="text-xs text-muted-foreground">
									{t("preferences.push.device.description")}
								</p>
							</div>
							<Switch
								aria-label={t("preferences.push.device.label")}
								checked={device.data ?? false}
								disabled={device.isPending || toggleDevice.isPending}
								onCheckedChange={(on) => toggleDevice.mutate({ on })}
							/>
						</div>
						{denied ? (
							<InlineNotice tone="error">
								{t("preferences.push.device.denied")}
							</InlineNotice>
						) : null}
						{toggleDevice.isError ? (
							<InlineNotice tone="error">
								{t(
									toggleDevice.variables?.on === false
										? "preferences.push.errors.disable"
										: "preferences.push.errors.enable",
								)}
							</InlineNotice>
						) : null}

						<div className="flex flex-col gap-3 border-t pt-4">
							<p className="text-sm font-medium">
								{t("preferences.push.types.title")}
							</p>
							{TYPE_ORDER.map((type) => (
								<div
									key={type}
									className="flex items-center justify-between gap-4"
								>
									<p className="text-sm">
										{t(`preferences.push.types.${type}`)}
									</p>
									<Switch
										size="sm"
										aria-label={t(`preferences.push.types.${type}`)}
										checked={preferences.get(type) ?? true}
										onCheckedChange={(enabled) =>
											setPreference.mutate({ type, enabled })
										}
									/>
								</div>
							))}
							{setPreference.isError ? (
								<InlineNotice tone="error">
									{t("preferences.push.errors.save")}
								</InlineNotice>
							) : null}
						</div>
					</>
				)}
			</CardContent>
		</Card>
	);
}
