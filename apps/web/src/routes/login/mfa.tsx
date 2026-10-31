import { createFileRoute, redirect } from "@tanstack/react-router";

import { LoginShell, MfaChallengePanel } from "@/components/auth/login/index";
import { currentUserQueryOptions } from "@/lib/auth-api";
import { loadPendingMfa } from "@/lib/mfa-challenge";

/** Code entry for a sign-in that owes a second factor. Reached only from the
 * login form, which leaves the challenge in sessionStorage; without one there
 * is nothing to verify, so it goes back to the login page. */
export const Route = createFileRoute("/login/mfa")({
	beforeLoad: async ({ context: { queryClient } }) => {
		const user = await queryClient
			.fetchQuery(currentUserQueryOptions)
			.catch(() => null);
		if (user) throw redirect({ to: "/home" });
		const pending = loadPendingMfa();
		if (!pending) throw redirect({ to: "/" });
		return { pending };
	},
	component: MfaPage,
});

function MfaPage() {
	const { pending } = Route.useRouteContext();
	return (
		<LoginShell>
			<MfaChallengePanel pending={pending} />
		</LoginShell>
	);
}
