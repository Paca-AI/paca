import { Skeleton } from "@/components/ui/skeleton";

export function UsersTableSkeleton() {
	return (
		<div
			className="overflow-hidden rounded-xl border"
			role="status"
			aria-busy="true"
		>
			<div className="border-b bg-muted/40 px-5 py-3">
				<div className="flex gap-4">
					<Skeleton className="h-3.5 w-20" />
					<Skeleton className="ml-auto h-3.5 w-16 sm:mr-24" />
					<Skeleton className="hidden h-3.5 w-14 md:block" />
				</div>
			</div>
			{["row-1", "row-2", "row-3", "row-4"].map((rowKey) => (
				<div
					key={rowKey}
					className="flex items-center gap-3 border-b px-5 py-3 last:border-0"
				>
					<Skeleton className="size-8 shrink-0 rounded-full" />
					<div className="min-w-0 flex-1 space-y-1.5">
						<Skeleton className="h-3.5 w-32" />
						<Skeleton className="h-3 w-20" />
					</div>
					<div className="flex gap-1">
						<Skeleton className="h-5 w-16 rounded-full" />
						<Skeleton className="h-5 w-10 rounded-full" />
					</div>
					<Skeleton className="hidden h-4 w-20 md:block" />
					<Skeleton className="hidden size-7 rounded-md sm:block" />
				</div>
			))}
		</div>
	);
}
