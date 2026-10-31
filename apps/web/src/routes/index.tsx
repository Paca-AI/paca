import { createFileRoute, redirect } from "@tanstack/react-router";

import { LoginFormPanel, LoginShell } from "@/components/auth/login/index";
import { currentUserQueryOptions } from "@/lib/auth-api";
import { sanitizeRedirect } from "@/lib/safe-redirect";

export const Route = createFileRoute("/")({
	// `sso_error` is set by the API when an SSO sign-in fails (see
	// SsoErrorNotice). `redirect` is where to go once signed in; it is only
	// honoured when it is a same-origin path (see sanitizeRedirect).
	validateSearch: (
		search: Record<string, unknown>,
	): { sso_error?: string; redirect?: string } => ({
		...(typeof search.sso_error === "string"
			? { sso_error: search.sso_error }
			: {}),
		...(sanitizeRedirect(search.redirect)
			? { redirect: sanitizeRedirect(search.redirect) as string }
			: {}),
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
	},
	component: LoginPage,
});

function LoginPage() {
	const { sso_error: ssoError, redirect } = Route.useSearch();
	return (
		<LoginShell>
			<LoginFormPanel ssoError={ssoError} redirectTo={redirect} />
		</LoginShell>
	);
}
