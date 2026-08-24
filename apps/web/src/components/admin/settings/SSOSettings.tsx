import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Loader2, Save, ShieldCheck } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import {
	type SSOSettings as SSOSettingsData,
	ssoSettingsQueryOptions,
	updateSSOSettings,
} from "@/lib/sso-settings-api";

interface SSOFormState {
	enabled: boolean;
	issuerURL: string;
	clientID: string;
	clientSecret: string;
	scopes: string;
	redirectURL: string;
	displayName: string;
	usernameClaim: string;
	localLoginEnabled: boolean;
}

function formFromSettings(settings?: SSOSettingsData): SSOFormState {
	return {
		enabled: settings?.enabled ?? false,
		issuerURL: settings?.issuer_url ?? "",
		clientID: settings?.client_id ?? "",
		clientSecret: "",
		scopes: settings?.scopes.join(", ") ?? "openid, profile, email",
		redirectURL: settings?.redirect_url ?? "",
		displayName: settings?.display_name ?? "Single Sign-On",
		usernameClaim: settings?.username_claim ?? "preferred_username",
		localLoginEnabled: settings?.local_login_enabled ?? true,
	};
}

function normalizedScopes(value: string): string[] {
	return [
		...new Set(
			value
				.split(/[\s,]+/)
				.map((scope) => scope.trim())
				.filter(Boolean),
		),
	];
}

export function SSOSettings() {
	const { t } = useTranslation("admin");
	const queryClient = useQueryClient();
	const { data: settings } = useQuery(ssoSettingsQueryOptions);
	const [form, setForm] = useState<SSOFormState>(() =>
		formFromSettings(settings),
	);
	const [error, setError] = useState<string | null>(null);
	const [saved, setSaved] = useState(false);

	useEffect(() => {
		setForm(formFromSettings(settings));
	}, [settings]);

	const mutation = useMutation({
		mutationFn: () =>
			updateSSOSettings({
				enabled: form.enabled,
				issuer_url: form.issuerURL,
				client_id: form.clientID,
				...(form.clientSecret ? { client_secret: form.clientSecret } : {}),
				scopes: normalizedScopes(form.scopes),
				redirect_url: form.redirectURL,
				display_name: form.displayName,
				username_claim: form.usernameClaim,
				local_login_enabled: form.localLoginEnabled,
			}),
		onSuccess: (updated) => {
			queryClient.setQueryData(ssoSettingsQueryOptions.queryKey, updated);
			setForm(formFromSettings(updated));
			setError(null);
			setSaved(true);
			setTimeout(() => setSaved(false), 2500);
		},
		onError: (cause) => {
			const code = getApiErrorCode(cause);
			const key =
				code === ApiErrorCode.SSOConfigInvalid
					? "settings.sso.errors.invalid"
					: code === ApiErrorCode.SSOProviderValidationFailed
						? "settings.sso.errors.provider"
						: code === ApiErrorCode.SSOEncryptionUnavailable
							? "settings.sso.errors.encryption"
							: code === ApiErrorCode.SSOAdminRequired
								? "settings.sso.errors.adminRequired"
								: "settings.sso.errors.updateFailed";
			setError(t(key));
			setSaved(false);
		},
	});

	if (!settings) return null;

	const baseline = formFromSettings(settings);
	const isDirty =
		form.clientSecret !== "" ||
		form.enabled !== baseline.enabled ||
		form.issuerURL !== baseline.issuerURL ||
		form.clientID !== baseline.clientID ||
		form.scopes !== baseline.scopes ||
		form.redirectURL !== baseline.redirectURL ||
		form.displayName !== baseline.displayName ||
		form.usernameClaim !== baseline.usernameClaim ||
		form.localLoginEnabled !== baseline.localLoginEnabled;

	function set<K extends keyof SSOFormState>(key: K, value: SSOFormState[K]) {
		setForm((current) => ({ ...current, [key]: value }));
		setSaved(false);
	}

	return (
		<form
			className="rounded-lg border border-border/60 bg-card"
			onSubmit={(event) => {
				event.preventDefault();
				mutation.mutate();
			}}
		>
			<div className="flex items-center justify-between gap-4 border-b border-border/60 px-5 py-4">
				<div className="flex min-w-0 items-center gap-3">
					<div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
						<ShieldCheck className="size-4" />
					</div>
					<div className="min-w-0">
						<h2 className="text-sm font-semibold">{t("settings.sso.title")}</h2>
						<p className="truncate text-xs text-muted-foreground">
							{t("settings.sso.source", { source: settings.source })}
						</p>
					</div>
				</div>
				<Switch
					id="sso-enabled"
					checked={form.enabled}
					onCheckedChange={(checked) => {
						set("enabled", checked);
						if (!checked) set("localLoginEnabled", true);
					}}
					aria-label={t("settings.sso.enabled")}
				/>
			</div>

			<div className="grid gap-5 px-5 py-5 sm:grid-cols-2">
				<div className="space-y-1.5 sm:col-span-2">
					<Label htmlFor="sso-issuer">{t("settings.sso.issuerURL")}</Label>
					<Input
						id="sso-issuer"
						value={form.issuerURL}
						onChange={(event) => set("issuerURL", event.target.value)}
						placeholder="https://id.example.com/realms/company"
					/>
				</div>

				<div className="space-y-1.5">
					<Label htmlFor="sso-client-id">{t("settings.sso.clientID")}</Label>
					<Input
						id="sso-client-id"
						value={form.clientID}
						onChange={(event) => set("clientID", event.target.value)}
					/>
				</div>

				<div className="space-y-1.5">
					<Label htmlFor="sso-client-secret">
						{t("settings.sso.clientSecret")}
					</Label>
					<Input
						id="sso-client-secret"
						type="password"
						autoComplete="new-password"
						value={form.clientSecret}
						onChange={(event) => set("clientSecret", event.target.value)}
						placeholder={
							settings.client_secret_configured
								? t("settings.sso.secretConfigured")
								: t("settings.sso.secretNotConfigured")
						}
					/>
					<p className="text-xs text-muted-foreground">
						{settings.client_secret_configured
							? t("settings.sso.secretConfigured")
							: t("settings.sso.secretNotConfigured")}
					</p>
				</div>

				<div className="space-y-1.5 sm:col-span-2">
					<Label htmlFor="sso-scopes">{t("settings.sso.scopes")}</Label>
					<Input
						id="sso-scopes"
						value={form.scopes}
						onChange={(event) => set("scopes", event.target.value)}
						placeholder="openid, profile, email"
					/>
				</div>

				<div className="space-y-1.5 sm:col-span-2">
					<Label htmlFor="sso-redirect">{t("settings.sso.redirectURL")}</Label>
					<Input
						id="sso-redirect"
						value={form.redirectURL}
						onChange={(event) => set("redirectURL", event.target.value)}
						placeholder="https://paca.example.com/api/v1/auth/oidc/callback"
					/>
				</div>

				<div className="space-y-1.5">
					<Label htmlFor="sso-display-name">
						{t("settings.sso.displayName")}
					</Label>
					<Input
						id="sso-display-name"
						value={form.displayName}
						onChange={(event) => set("displayName", event.target.value)}
					/>
				</div>

				<div className="space-y-1.5">
					<Label htmlFor="sso-username-claim">
						{t("settings.sso.usernameClaim")}
					</Label>
					<Input
						id="sso-username-claim"
						value={form.usernameClaim}
						onChange={(event) => set("usernameClaim", event.target.value)}
					/>
				</div>
			</div>

			<div className="flex items-center justify-between gap-4 border-t border-border/60 px-5 py-4">
				<Label htmlFor="local-login-enabled" className="min-w-0">
					<span className="block">{t("settings.sso.localLogin")}</span>
					<span className="block text-xs font-normal text-muted-foreground">
						{t("settings.sso.localLoginDescription")}
					</span>
				</Label>
				<Switch
					id="local-login-enabled"
					checked={form.localLoginEnabled}
					disabled={!form.enabled}
					onCheckedChange={(checked) => set("localLoginEnabled", checked)}
				/>
			</div>

			{!settings.encrypted_secret_storage_available ? (
				<div className="mx-5 mb-4 flex gap-2 rounded-lg bg-destructive/10 px-3 py-2 text-xs text-destructive">
					<AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
					<span>{t("settings.sso.errors.encryption")}</span>
				</div>
			) : null}
			{error ? (
				<p className="mx-5 mb-4 rounded-lg bg-destructive/10 px-3 py-2 text-xs text-destructive">
					{error}
				</p>
			) : null}

			<div className="flex items-center gap-2 border-t border-border/60 px-5 py-4">
				<Button
					type="submit"
					size="sm"
					disabled={
						!settings.encrypted_secret_storage_available ||
						!isDirty ||
						mutation.isPending
					}
				>
					{mutation.isPending ? <Loader2 className="animate-spin" /> : <Save />}
					{t("settings.sso.save")}
				</Button>
				{saved ? (
					<span className="text-xs font-medium text-emerald-600 dark:text-emerald-400">
						{t("settings.sso.saved")}
					</span>
				) : null}
			</div>
		</form>
	);
}
