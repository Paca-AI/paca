import { Plus, Users } from "lucide-react";

import { Button } from "@/components/ui/button";

interface EmptyUsersStateProps {
	canWrite: boolean;
	onCreate: () => void;
}

export function EmptyUsersState({ canWrite, onCreate }: EmptyUsersStateProps) {
	return (
		<div className="flex flex-col items-center gap-4 rounded-xl border border-dashed bg-muted/20 py-16 text-center">
			<div className="flex size-12 items-center justify-center rounded-full bg-muted text-muted-foreground/60">
				<Users className="size-6" />
			</div>
			<div>
				<p className="text-sm font-medium">No users found</p>
				<p className="mt-1 text-xs text-muted-foreground">
					Create your first user to get started.
				</p>
			</div>
			{canWrite ? (
				<Button size="sm" variant="outline" onClick={onCreate}>
					<Plus className="size-4" />
					Create user
				</Button>
			) : null}
		</div>
	);
}

export function UsersErrorState() {
	return (
		<div className="flex flex-col items-center gap-3 rounded-xl border border-destructive/20 bg-destructive/5 py-14 text-center">
			<Users className="size-8 text-destructive/40" />
			<div>
				<p className="text-sm font-medium text-destructive">
					Failed to load users
				</p>
				<p className="mt-0.5 text-xs text-muted-foreground">
					Please refresh the page and try again.
				</p>
			</div>
		</div>
	);
}

export function UsersNoPermissionState() {
	return (
		<div className="flex flex-col items-center gap-3 rounded-xl border border-amber-200 bg-amber-50 py-14 text-center dark:border-amber-900/40 dark:bg-amber-900/10">
			<Users className="size-8 text-amber-400 dark:text-amber-500" />
			<div>
				<p className="text-sm font-medium text-amber-700 dark:text-amber-400">
					You don't have permission to view users
				</p>
				<p className="mt-0.5 text-xs text-muted-foreground">
					You can still create new users using the button above.
				</p>
			</div>
		</div>
	);
}
