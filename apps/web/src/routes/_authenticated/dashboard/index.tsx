import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { currentUserQueryOptions, logout } from "@/lib/auth-api";

export const Route = createFileRoute("/_authenticated/dashboard/")({
	component: RouteComponent,
});

function RouteComponent() {
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const [isLoggingOut, setIsLoggingOut] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const handleLogout = async () => {
		setError(null);
		setIsLoggingOut(true);

		try {
			await logout();
			await queryClient.invalidateQueries({
				queryKey: currentUserQueryOptions.queryKey,
			});
			await navigate({ to: "/" });
		} catch {
			setError("Logout failed. Please try again.");
		} finally {
			setIsLoggingOut(false);
		}
	};

	return (
		<div className="mx-auto flex min-h-screen w-full max-w-5xl flex-col gap-4 px-4 py-8 sm:px-8">
			<div className="flex items-center justify-between">
				<h1 className="text-2xl font-semibold">Dashboard</h1>
				<Button
					onClick={handleLogout}
					variant="destructive"
					disabled={isLoggingOut}
				>
					{isLoggingOut ? "Logging out..." : "Temporary Logout"}
				</Button>
			</div>

			{error ? <p className="text-sm text-destructive">{error}</p> : null}
		</div>
	);
}
