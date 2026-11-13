import { createFileRoute, redirect } from "@tanstack/react-router";

import { LoginFormPanel, LoginShell } from "@/components/auth/login/index";
import { currentUserQueryOptions } from "@/lib/auth-api";
import { loginOptionsQueryOptions } from "@/lib/mfa-api";
import { sanitizeRedirect } from "@/lib/safe-redirect";

/** `?admin=1` and `?admin=true` both turn the flag on. */
function isSet(value: unknown): boolean {
	return value === true || value === 1 || value === "1" || value === "true";
}

export const Route = createFileRoute("/")({
	// `sso_error` is set by the API when an SSO sign-in fails (see
	// SsoErrorNotice). `redirect` is where to go once signed in; it is only
	// honoured when it is a same-origin path (see sanitizeRedirect). `admin`
	// opens the password form when the instance is SSO-only, for the default
	// super admin (see LoginFormPanel).
	validateSearch: (
		search: Record<string, unknown>,
	): { sso_error?: string; redirect?: string; admin?: true } => ({
		...(typeof search.sso_error === "string"
			? { sso_error: search.sso_error }
			: {}),
		...(sanitizeRedirect(search.redirect)
			? { redirect: sanitizeRedirect(search.redirect) as string }
			: {}),
		...(isSet(search.admin) ? { admin: true as const } : {}),
	}),
	beforeLoad: async ({ context: { queryClient }, search }) => {
		const user = await queryClient
			.fetchQuery(currentUserQueryOptions)
			.catch(() => null);
		if (user) {
			// Already signed in (e.g. the session was refreshed meanwhile).
			if (search.redirect) throw redirect({ href: search.redirect });
			throw redirect({ to: "/home" });
		}
		// Loaded before the page renders, so an SSO-only instance never
		// flashes the password form. A failed lookup shows the form: the API
		// enforces the policy either way.
		await queryClient
			.ensureQueryData(loginOptionsQueryOptions)
			.catch(() => null);
	},
	component: LoginPage,
});

function LoginPage() {
	const { sso_error: ssoError, redirect, admin } = Route.useSearch();
	return (
		<LoginShell>
			<LoginFormPanel
				ssoError={ssoError}
				redirectTo={redirect}
				adminSignIn={admin}
			/>
		</LoginShell>
	);
}
