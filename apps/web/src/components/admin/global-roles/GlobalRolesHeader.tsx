import { Plus, Shield } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";

interface GlobalRolesHeaderProps {
	canWrite: boolean;
	onCreate: () => void;
}

export function GlobalRolesHeader({
	canWrite,
	onCreate,
}: GlobalRolesHeaderProps) {
	const { t } = useTranslation();
	return (
		<div className="flex items-start justify-between gap-4">
			<div>
				<div className="mb-1 flex items-center gap-2.5">
					<div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
						<Shield className="size-4" />
					</div>
					<h1 className="text-xl font-semibold tracking-tight">
						{t("admin.globalRoles.title")}
					</h1>
				</div>
				<p className="ml-10 text-sm text-muted-foreground">
					{t("admin.globalRoles.subtitle")}
				</p>
			</div>
			{canWrite ? (
				<Button size="sm" onClick={onCreate} className="shrink-0">
					<Plus className="size-4" />
					{t("admin.globalRoles.newRole")}
				</Button>
			) : null}
		</div>
	);
}
