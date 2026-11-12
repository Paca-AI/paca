import { Plus, Shield } from "lucide-react";
import { useTranslation } from "react-i18next";
import { ReportIssueButton } from "@/components/report-issue-button";
import { Button } from "@/components/ui/button";
import {
	Empty,
	EmptyContent,
	EmptyDescription,
	EmptyHeader,
	EmptyMedia,
	EmptyTitle,
} from "@/components/ui/empty";

interface EmptyRolesStateProps {
	canWrite: boolean;
	onCreate: () => void;
}

export function EmptyRolesState({ canWrite, onCreate }: EmptyRolesStateProps) {
	const { t } = useTranslation("admin");

	return (
		<Empty className="border border-dashed bg-muted/20 py-16 ">
			<EmptyHeader>
				<EmptyMedia variant="icon">
					<Shield className="size-6" />
				</EmptyMedia>
				<EmptyTitle>{t("globalRoles.empty.title")}</EmptyTitle>
				<EmptyDescription>
					{t("globalRoles.empty.description")}
				</EmptyDescription>
			</EmptyHeader>
			<EmptyContent>
				{canWrite ? (
					<Button size="sm" variant="outline" onClick={onCreate}>
						<Plus className="size-4" />
						{t("globalRoles.empty.createRole")}
					</Button>
				) : null}
			</EmptyContent>
		</Empty>
	);
}

export function GlobalRolesErrorState({ error }: { error?: unknown }) {
	const { t } = useTranslation("admin");

	return (
		<div className="flex flex-col items-center gap-3 rounded-xl border border-destructive/20 bg-destructive/5 py-14 text-center">
			<Shield className="size-8 text-destructive/40" />
			<div>
				<p className="text-sm font-medium text-destructive">
					{t("globalRoles.errors.loadFailed")}
				</p>
				<p className="mt-0.5 text-xs text-muted-foreground">
					{t("globalRoles.errors.tryAgain")}
				</p>
			</div>
			<ReportIssueButton error={error} />
		</div>
	);
}
