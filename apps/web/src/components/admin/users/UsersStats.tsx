import { Shield, Users } from "lucide-react";

import { Separator } from "@/components/ui/separator";

interface UsersStatsProps {
	total: number;
	mustChangePasswordCount: number;
}

export function UsersStats({
	total,
	mustChangePasswordCount,
}: UsersStatsProps) {
	return (
		<div className="flex items-center gap-5 rounded-xl border bg-muted/20 px-5 py-3">
			<div className="flex items-center gap-2">
				<Users className="size-4 text-primary" />
				<span className="text-sm">
					<span className="font-semibold tabular-nums">{total}</span>
					<span className="ml-1.5 text-muted-foreground">
						{total === 1 ? "user" : "users"} in system
					</span>
				</span>
			</div>
			{mustChangePasswordCount > 0 ? (
				<>
					<Separator orientation="vertical" className="h-4" />
					<div className="flex items-center gap-2">
						<Shield className="size-4 text-amber-500" />
						<span className="text-sm">
							<span className="font-semibold tabular-nums text-amber-600">
								{mustChangePasswordCount}
							</span>
							<span className="ml-1.5 text-muted-foreground">
								must change password
							</span>
						</span>
					</div>
				</>
			) : null}
		</div>
	);
}
