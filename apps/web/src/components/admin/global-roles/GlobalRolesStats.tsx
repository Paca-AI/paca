import { Key, Shield } from "lucide-react";

import { Separator } from "@/components/ui/separator";

interface GlobalRolesStatsProps {
	rolesCount: number;
	totalGranted: number;
}

export function GlobalRolesStats({
	rolesCount,
	totalGranted,
}: GlobalRolesStatsProps) {
	return (
		<div className="flex items-center gap-5 rounded-xl border bg-muted/20 px-5 py-3">
			<div className="flex items-center gap-2">
				<Shield className="size-4 text-primary" />
				<span className="text-sm">
					<span className="font-semibold tabular-nums">{rolesCount}</span>
					<span className="ml-1.5 text-muted-foreground">
						{rolesCount === 1 ? "role" : "roles"} defined
					</span>
				</span>
			</div>
			<Separator orientation="vertical" className="h-4" />
			<div className="flex items-center gap-2">
				<Key className="size-4 text-muted-foreground" />
				<span className="text-sm">
					<span className="font-semibold tabular-nums">{totalGranted}</span>
					<span className="ml-1.5 text-muted-foreground">
						permission grants across all roles
					</span>
				</span>
			</div>
		</div>
	);
}
