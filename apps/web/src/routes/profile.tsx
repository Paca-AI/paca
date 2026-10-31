import { createFileRoute, redirect } from "@tanstack/react-router";

// "My Profile" became Account settings (/account); keep old bookmarks working.
export const Route = createFileRoute("/profile")({
	beforeLoad: () => {
		throw redirect({ to: "/account", replace: true });
	},
});
