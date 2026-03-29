import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/dashboard/")({
	beforeLoad: () => {
		throw redirect({ to: "/home" });
	},
	component: () => null,
});
