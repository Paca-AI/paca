import { useTranslation } from "react-i18next";

import { Badge } from "@/components/ui/badge";
import type { SprintStatus } from "@/lib/interaction-api";
import { cn } from "@/lib/utils";

const STATUS_STYLES: Record<SprintStatus, string> = {
	planned: "bg-muted/60 text-muted-foreground border-transparent",
	active:
		"bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-transparent",
	completed:
		"bg-blue-500/10 text-blue-600 dark:text-blue-400 border-transparent",
};

export function SprintStatusBadge({
	status,
	className,
}: {
	status: SprintStatus;
	className?: string;
}) {
	const { t } = useTranslation("projects");
	const label =
		status === "active"
			? t("layout.sprintDetail.statusActive")
			: status === "planned"
				? t("layout.sprintDetail.statusPlanned")
				: t("layout.sprintDetail.statusCompleted");

	return (
		<Badge variant="outline" className={cn(STATUS_STYLES[status], className)}>
			{label}
		</Badge>
	);
}
