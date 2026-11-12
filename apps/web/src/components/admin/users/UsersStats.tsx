import { Shield, Users } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Separator } from "@/components/ui/separator";

interface UsersStatsProps {
	total: number;
	mustChangePasswordCount: number;
}

export function UsersStats({
	total,
	mustChangePasswordCount,
}: UsersStatsProps) {
	const { t } = useTranslation("admin");

	return (
		<div className="flex items-center gap-5 rounded-xl border bg-muted/20 px-5 py-3">
			<div className="flex items-center gap-2">
				<Users className="size-4 text-primary" />
				<span className="text-sm">
					<span className="font-semibold tabular-nums">{total}</span>
					<span className="ml-1.5 text-muted-foreground">
						{t("users.stats.userCount", { count: total })}{" "}
						{t("users.stats.inSystem")}
					</span>
				</span>
			</div>
			{mustChangePasswordCount > 0 ? (
				<>
					<Separator orientation="vertical" className="h-4" />
					<div className="flex items-center gap-2">
						<Shield className="size-4 text-warning" />
						<span className="text-sm">
							<span className="font-semibold tabular-nums text-warning">
								{mustChangePasswordCount}
							</span>
							<span className="ml-1.5 text-muted-foreground">
								{t("users.stats.mustChangePassword")}
							</span>
						</span>
					</div>
				</>
			) : null}
		</div>
	);
}
