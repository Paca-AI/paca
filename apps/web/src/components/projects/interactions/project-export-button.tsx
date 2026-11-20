import { useMutation } from "@tanstack/react-query";
import { Download } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Spinner } from "@/components/ui/spinner";
import { toast } from "@/components/ui/toast";
import { showErrorToast } from "@/lib/notify";
import {
	buildProjectTasksCsv,
	downloadCsv,
} from "@/lib/task-csv-export";
import { cn } from "@/lib/utils";

/**
 * One-click Timeline/Board export: fetches every task in the project and
 * downloads an Excel-ready CSV immediately (no Settings / archive round-trip).
 */
export function ProjectExportButton({
	projectId,
	canExport,
}: {
	projectId: string;
	canExport: boolean;
}) {
	const { t } = useTranslation("projects");

	const exportMutation = useMutation({
		mutationFn: () => buildProjectTasksCsv(projectId),
		onSuccess: ({ csv, fileName, rowCount }) => {
			downloadCsv(csv, fileName);
			toast.add({
				type: "success",
				title: t("layout.shell.exportReady", { count: rowCount }),
				timeout: 4000,
			});
		},
		onError: () => {
			showErrorToast({
				title: t("layout.shell.exportFailed"),
				description: "",
			});
		},
	});

	if (!canExport) return null;

	const isBusy = exportMutation.isPending;
	const label = isBusy
		? t("layout.shell.exporting")
		: t("layout.shell.export");

	return (
		<button
			type="button"
			disabled={isBusy}
			onClick={() => exportMutation.mutate()}
			title={t("layout.shell.exportProject")}
			aria-label={label}
			className={cn(
				"inline-flex h-7 items-center gap-1.5 rounded-md border border-border/50 bg-muted/15 px-2.5 text-xs font-semibold transition-all duration-150",
				isBusy
					? "border-primary/40 text-primary"
					: "text-foreground/80 hover:bg-muted/40 hover:text-foreground",
				"disabled:opacity-70",
			)}
		>
			{isBusy ? (
				<Spinner className="size-3.5 shrink-0" />
			) : (
				<Download className="size-3.5 shrink-0" />
			)}
			<span>{label}</span>
		</button>
	);
}
