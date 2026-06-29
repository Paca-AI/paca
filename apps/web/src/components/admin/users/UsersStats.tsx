import { Shield, Users } from "lucide-react";
import { Trans, useTranslation } from "react-i18next";

import { Separator } from "@/components/ui/separator";

interface UsersStatsProps {
	total: number;
	mustChangePasswordCount: number;
}

export function UsersStats({
	total,
	mustChangePasswordCount,
}: UsersStatsProps) {
	const { t } = useTranslation();
	return (
		<div className="flex items-center gap-5 rounded-xl border bg-muted/20 px-5 py-3">
			<div className="flex items-center gap-2">
				<Users className="size-4 text-primary" />
				<span className="text-sm">
					<Trans
						i18nKey="admin.users.usersInSystem"
						count={total}
						components={{
							n: <span className="font-semibold tabular-nums" />,
							m: <span className="ml-1.5 text-muted-foreground" />,
						}}
					/>
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
								{t("admin.users.mustChangePassword")}
							</span>
						</span>
					</div>
				</>
			) : null}
		</div>
	);
}
