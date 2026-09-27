import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";

import { InlineNotice } from "@/components/shared/inline-notice";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogClose,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import {
	createSsoProvider,
	type SsoProvider,
	type SsoProviderInput,
	ssoProvidersQueryOptions,
	updateSsoProvider,
} from "@/lib/sso-api";

import { CopyableValue } from "./CopyableValue";

interface SsoProviderDialogProps {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	/** The provider to edit; omitted to create a new one. */
	provider?: SsoProvider;
}

/** Derives a URL-safe slug from a display name ("Google Workspace" → "google-workspace"). */
export function slugify(name: string): string {
	return name
		.toLowerCase()
		.normalize("NFKD")
		.replace(/[^a-z0-9]+/g, "-")
		.replace(/^-+|-+$/g, "")
		.slice(0, 40)
		.replace(/-+$/g, "");
}

function splitList(value: string): string[] {
	return value
		.split(/[\s,]+/)
		.map((v) => v.trim())
		.filter(Boolean);
}

export function SsoProviderDialog({
	open,
	onOpenChange,
	provider,
}: SsoProviderDialogProps) {
	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-lg max-h-[90dvh] overflow-y-auto">
				{/* Keyed so reopening the dialog starts from fresh values. */}
				{open ? (
					<ProviderForm
						key={provider?.id ?? "new"}
						provider={provider}
						onDone={() => onOpenChange(false)}
					/>
				) : null}
			</DialogContent>
		</Dialog>
	);
}

function ProviderForm({
	provider,
	onDone,
}: {
	provider?: SsoProvider;
	onDone: () => void;
}) {
	const { t } = useTranslation("admin");
	const queryClient = useQueryClient();
	const id = useId();
	const isEdit = provider !== undefined;

	const [displayName, setDisplayName] = useState(provider?.display_name ?? "");
	const [slug, setSlug] = useState(provider?.slug ?? "");
	const [slugTouched, setSlugTouched] = useState(isEdit);
	const [issuerUrl, setIssuerUrl] = useState(provider?.issuer_url ?? "");
	const [clientId, setClientId] = useState(provider?.client_id ?? "");
	const [clientSecret, setClientSecret] = useState("");
	const [scopes, setScopes] = useState(
		(provider?.scopes ?? ["openid", "profile", "email"]).join(" "),
	);
	const [allowedDomains, setAllowedDomains] = useState(
		(provider?.allowed_domains ?? []).join(", "),
	);
	const [enabled, setEnabled] = useState(provider?.enabled ?? true);
	const [autoProvision, setAutoProvision] = useState(
		provider?.auto_provision ?? true,
	);
	const [linkByEmail, setLinkByEmail] = useState(
		provider?.link_by_email ?? false,
	);
	const [error, setError] = useState<string | null>(null);

	const effectiveSlug = slugTouched ? slug : slugify(displayName);
	const callbackUrl =
		provider && provider.slug === effectiveSlug
			? provider.callback_url
			: `${window.location.origin}/api/v1/auth/sso/${effectiveSlug || "…"}/callback`;

	const mutation = useMutation({
		mutationFn: () => {
			const input: SsoProviderInput = {
				slug: effectiveSlug,
				display_name: displayName,
				issuer_url: issuerUrl,
				client_id: clientId,
				// Blank on edit keeps the stored secret.
				client_secret: isEdit && clientSecret === "" ? null : clientSecret,
				scopes: splitList(scopes),
				enabled,
				auto_provision: autoProvision,
				link_by_email: linkByEmail,
				allowed_domains: splitList(allowedDomains),
			};
			return isEdit
				? updateSsoProvider(provider.id, input)
				: createSsoProvider(input);
		},
		onSuccess: async () => {
			await queryClient.invalidateQueries({
				queryKey: ssoProvidersQueryOptions.queryKey,
			});
			await queryClient.invalidateQueries({ queryKey: ["sso"] });
			onDone();
		},
		onError: (err) => {
			const code = getApiErrorCode(err);
			if (code === ApiErrorCode.SSOProviderSlugTaken) {
				setError(t("settings.sso.errors.slugTaken"));
			} else if (code === ApiErrorCode.SSODiscoveryFailed) {
				setError(t("settings.sso.errors.discoveryFailed"));
			} else if (code === ApiErrorCode.SSOProviderInvalid) {
				setError(t("settings.sso.errors.invalid"));
			} else {
				setError(t("settings.sso.errors.saveFailed"));
			}
		},
	});

	const canSave =
		displayName.trim() !== "" &&
		effectiveSlug !== "" &&
		issuerUrl.trim() !== "" &&
		clientId.trim() !== "" &&
		!mutation.isPending;

	return (
		<form
			onSubmit={(e) => {
				e.preventDefault();
				setError(null);
				mutation.mutate();
			}}
			className="flex flex-col gap-4"
		>
			<DialogHeader>
				<DialogTitle className="text-base">
					{isEdit
						? t("settings.sso.dialog.editTitle")
						: t("settings.sso.dialog.createTitle")}
				</DialogTitle>
				<DialogDescription>
					{t("settings.sso.dialog.description")}
				</DialogDescription>
			</DialogHeader>

			<Field id={`${id}-name`} label={t("settings.sso.fields.displayName")}>
				<Input
					id={`${id}-name`}
					value={displayName}
					onChange={(e) => setDisplayName(e.target.value)}
					placeholder={t("settings.sso.fields.displayNamePlaceholder")}
					required
				/>
			</Field>

			<Field
				id={`${id}-slug`}
				label={t("settings.sso.fields.slug")}
				hint={t("settings.sso.fields.slugHint")}
			>
				<Input
					id={`${id}-slug`}
					value={effectiveSlug}
					onChange={(e) => {
						setSlugTouched(true);
						setSlug(e.target.value.toLowerCase());
					}}
					pattern="[a-z0-9](?:[a-z0-9\-]{0,38}[a-z0-9])?"
					required
				/>
			</Field>

			<Field
				id={`${id}-callback`}
				label={t("settings.sso.fields.callbackUrl")}
				hint={t("settings.sso.fields.callbackUrlHint")}
			>
				<CopyableValue id={`${id}-callback`} value={callbackUrl} />
			</Field>

			<Field
				id={`${id}-issuer`}
				label={t("settings.sso.fields.issuerUrl")}
				hint={t("settings.sso.fields.issuerUrlHint")}
			>
				<Input
					id={`${id}-issuer`}
					type="url"
					value={issuerUrl}
					onChange={(e) => setIssuerUrl(e.target.value)}
					placeholder="https://accounts.google.com"
					required
				/>
			</Field>

			<Field id={`${id}-client-id`} label={t("settings.sso.fields.clientId")}>
				<Input
					id={`${id}-client-id`}
					value={clientId}
					onChange={(e) => setClientId(e.target.value)}
					autoComplete="off"
					required
				/>
			</Field>

			<Field
				id={`${id}-client-secret`}
				label={t("settings.sso.fields.clientSecret")}
				hint={
					isEdit && provider.has_client_secret
						? t("settings.sso.fields.clientSecretKeepHint")
						: undefined
				}
			>
				<Input
					id={`${id}-client-secret`}
					type="password"
					value={clientSecret}
					onChange={(e) => setClientSecret(e.target.value)}
					autoComplete="new-password"
					placeholder={
						isEdit && provider.has_client_secret ? "••••••••" : undefined
					}
				/>
			</Field>

			<Field
				id={`${id}-scopes`}
				label={t("settings.sso.fields.scopes")}
				hint={t("settings.sso.fields.scopesHint")}
			>
				<Input
					id={`${id}-scopes`}
					value={scopes}
					onChange={(e) => setScopes(e.target.value)}
				/>
			</Field>

			<Field
				id={`${id}-domains`}
				label={t("settings.sso.fields.allowedDomains")}
				hint={t("settings.sso.fields.allowedDomainsHint")}
			>
				<Input
					id={`${id}-domains`}
					value={allowedDomains}
					onChange={(e) => setAllowedDomains(e.target.value)}
					placeholder="example.com"
				/>
			</Field>

			<div className="flex flex-col gap-3 rounded-lg border border-border/60 p-3">
				<Toggle
					id={`${id}-enabled`}
					label={t("settings.sso.fields.enabled")}
					hint={t("settings.sso.fields.enabledHint")}
					checked={enabled}
					onChange={setEnabled}
				/>
				<Toggle
					id={`${id}-provision`}
					label={t("settings.sso.fields.autoProvision")}
					hint={t("settings.sso.fields.autoProvisionHint")}
					checked={autoProvision}
					onChange={setAutoProvision}
				/>
				<Toggle
					id={`${id}-link`}
					label={t("settings.sso.fields.linkByEmail")}
					hint={t("settings.sso.fields.linkByEmailHint")}
					checked={linkByEmail}
					onChange={setLinkByEmail}
				/>
				{linkByEmail ? (
					<InlineNotice tone="warning">
						{t("settings.sso.fields.linkByEmailWarning")}
					</InlineNotice>
				) : null}
			</div>

			{error ? <InlineNotice tone="error">{error}</InlineNotice> : null}

			<DialogFooter>
				<DialogClose render={<Button type="button" variant="outline" />}>
					{t("settings.sso.dialog.cancel")}
				</DialogClose>
				<Button type="submit" disabled={!canSave} className="gap-1.5">
					{mutation.isPending ? (
						<Loader2 className="size-3.5 animate-spin" />
					) : null}
					{t("settings.sso.dialog.save")}
				</Button>
			</DialogFooter>
		</form>
	);
}

function Field({
	id,
	label,
	hint,
	children,
}: {
	id: string;
	label: string;
	hint?: string;
	children: React.ReactNode;
}) {
	return (
		<div className="space-y-1.5">
			<Label htmlFor={id}>{label}</Label>
			{children}
			{hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
		</div>
	);
}

function Toggle({
	id,
	label,
	hint,
	checked,
	onChange,
}: {
	id: string;
	label: string;
	hint: string;
	checked: boolean;
	onChange: (checked: boolean) => void;
}) {
	return (
		<div className="flex items-start justify-between gap-4">
			<div>
				<Label htmlFor={id} className="cursor-pointer">
					{label}
				</Label>
				<p className="mt-0.5 text-xs text-muted-foreground">{hint}</p>
			</div>
			<Switch id={id} checked={checked} onCheckedChange={onChange} />
		</div>
	);
}
