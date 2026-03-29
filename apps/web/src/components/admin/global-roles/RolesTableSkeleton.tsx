import { Skeleton } from "@/components/ui/skeleton";

export function RolesTableSkeleton() {
	return (
		<div className="rounded-xl border overflow-hidden">
			<div className="border-b bg-muted/40 px-4 py-3">
				<div className="flex gap-4">
					<Skeleton className="h-3.5 w-16" />
					<Skeleton className="h-3.5 w-24" />
					<Skeleton className="ml-auto h-3.5 w-14" />
				</div>
			</div>
			{["row-1", "row-2", "row-3"].map((rowKey) => (
				<div
					key={rowKey}
					className="flex items-center gap-4 border-b px-4 py-4 last:border-0"
				>
					<Skeleton className="h-5 w-36 rounded-md" />
					<div className="flex flex-1 gap-1.5">
						<Skeleton className="h-5 w-28 rounded-full" />
						<Skeleton className="h-5 w-24 rounded-full" />
					</div>
					<Skeleton className="h-4 w-20" />
					<div className="flex gap-1.5">
						<Skeleton className="size-7 rounded-md" />
						<Skeleton className="size-7 rounded-md" />
					</div>
				</div>
			))}
		</div>
	);
}
