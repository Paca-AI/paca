import { Plus, Shield } from "lucide-react";

import { Button } from "@/components/ui/button";

interface EmptyRolesStateProps {
	canWrite: boolean;
	onCreate: () => void;
}

export function EmptyRolesState({ canWrite, onCreate }: EmptyRolesStateProps) {
	return (
		<div className="flex flex-col items-center gap-4 rounded-xl border border-dashed bg-muted/20 py-16 text-center">
			<div className="flex size-12 items-center justify-center rounded-full bg-muted text-muted-foreground/60">
				<Shield className="size-6" />
			</div>
			<div>
				<p className="text-sm font-medium">No roles defined yet</p>
				<p className="mt-1 text-xs text-muted-foreground">
					Create your first role to start managing user permissions.
				</p>
			</div>
			{canWrite ? (
				<Button size="sm" variant="outline" onClick={onCreate}>
					<Plus className="size-4" />
					Create role
				</Button>
			) : null}
		</div>
	);
}

export function GlobalRolesErrorState() {
	return (
		<div className="flex flex-col items-center gap-3 rounded-xl border border-destructive/20 bg-destructive/5 py-14 text-center">
			<Shield className="size-8 text-destructive/40" />
			<div>
				<p className="text-sm font-medium text-destructive">
					Failed to load roles
				</p>
				<p className="mt-0.5 text-xs text-muted-foreground">
					Please refresh the page and try again.
				</p>
			</div>
		</div>
	);
}

export function GlobalRolesNoPermissionState() {
	return (
		<div className="flex flex-col items-center gap-3 rounded-xl border border-amber-200 bg-amber-50 py-14 text-center dark:border-amber-900/40 dark:bg-amber-900/10">
			<Shield className="size-8 text-amber-400 dark:text-amber-500" />
			<div>
				<p className="text-sm font-medium text-amber-700 dark:text-amber-400">
					You don't have permission to view roles
				</p>
				<p className="mt-0.5 text-xs text-muted-foreground">
					You can still create new roles using the button above.
				</p>
			</div>
		</div>
	);
}
