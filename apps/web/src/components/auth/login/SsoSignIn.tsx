import { useQuery } from "@tanstack/react-query";
import { AlertCircle, KeyRound } from "lucide-react";
import { useTranslation } from "react-i18next";

import { buttonVariants } from "@/components/ui/button";
import { publicSsoProvidersQueryOptions, ssoLoginUrl } from "@/lib/sso-api";
import { cn } from "@/lib/utils";

/** Codes the API's SSO callback redirects back with (?sso_error=…). */
const SSO_ERROR_CODES = [
	"provider_not_found",
	"provider_unavailable",
	"provider_denied",
	"state_invalid",
	"exchange_failed",
	"email_not_allowed",
	"no_account",
	"account_exists",
	"rate_limited",
] as const;

type SsoErrorKey = (typeof SSO_ERROR_CODES)[number] | "generic";

function ssoErrorKey(code: string): SsoErrorKey {
	return (SSO_ERROR_CODES as readonly string[]).includes(code)
		? (code as SsoErrorKey)
		: "generic";
}

export function SsoErrorNotice({ code }: { code?: string }) {
	const { t } = useTranslation("auth");
	if (!code) return null;
	const key = ssoErrorKey(code);
	return (
		<div
			role="alert"
			className="mb-5 flex items-start gap-2.5 rounded-lg border border-red-200 bg-red-50 px-3.5 py-3 text-sm text-red-700 dark:border-red-800/60 dark:bg-red-950/30 dark:text-red-400"
		>
			<AlertCircle className="mt-px size-4 shrink-0" />
			<span>{t(`login.sso.errors.${key}`)}</span>
		</div>
	);
}

/**
 * "Continue with …" buttons for every enabled SSO provider. Renders nothing
 * when none is configured, so the login page is unchanged by default.
 */
export function SsoSignInButtons({ rememberMe }: { rememberMe: boolean }) {
	const { t } = useTranslation("auth");
	const { data: providers } = useQuery(publicSsoProvidersQueryOptions);
	if (!providers?.length) return null;

	return (
		<div className="mt-6">
			<div className="mb-4 flex items-center gap-3 text-xs text-(--sea-ink-soft)">
				<span className="h-px flex-1 bg-(--line)" />
				{t("login.sso.divider")}
				<span className="h-px flex-1 bg-(--line)" />
			</div>
			<div className="space-y-2.5">
				{providers.map((p) => (
					<a
						key={p.slug}
						href={ssoLoginUrl(p.slug, rememberMe)}
						className={cn(
							buttonVariants({ variant: "outline", size: "lg" }),
							"h-11 w-full gap-2 font-medium",
						)}
					>
						<KeyRound className="size-4" />
						{t("login.sso.continueWith", { provider: p.display_name })}
					</a>
				))}
			</div>
		</div>
	);
}
