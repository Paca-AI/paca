import { createFileRoute } from "@tanstack/react-router";
import { Users } from "lucide-react";

import { Separator } from "@/components/ui/separator";

export const Route = createFileRoute("/_authenticated/admin/users/")({
	component: UsersManagementPage,
});

function UsersManagementPage() {
	return (
		<div className="flex flex-col gap-6 p-6 max-w-5xl w-full mx-auto">
			<div>
				<div className="flex items-center gap-2">
					<Users className="size-5 text-primary" />
					<h1 className="text-xl font-semibold">User Management</h1>
				</div>
				<p className="mt-1 text-sm text-muted-foreground">
					View and manage user accounts across the system.
				</p>
			</div>
			<Separator />
			<div className="flex flex-col items-center gap-3 py-16 text-center">
				<Users className="size-10 text-muted-foreground/40" />
				<div>
					<p className="text-sm font-medium">Coming soon</p>
					<p className="text-xs text-muted-foreground mt-0.5">
						User management is not yet available in this version.
					</p>
				</div>
			</div>
		</div>
	);
}
