import { Plus, Users } from "lucide-react";
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

interface EmptyUsersStateProps {
	canWrite: boolean;
	onCreate: () => void;
}

export function EmptyUsersState({ canWrite, onCreate }: EmptyUsersStateProps) {
	const { t } = useTranslation("admin");

	return (
		<Empty className="border border-dashed bg-muted/20 py-16 ">
			<EmptyHeader>
				<EmptyMedia variant="icon">
					<Users className="size-6" />
				</EmptyMedia>
				<EmptyTitle>{t("users.empty.title")}</EmptyTitle>
				<EmptyDescription>{t("users.empty.description")}</EmptyDescription>
			</EmptyHeader>
			<EmptyContent>
				{canWrite ? (
					<Button size="sm" variant="outline" onClick={onCreate}>
						<Plus className="size-4" />
						{t("users.empty.createUser")}
					</Button>
				) : null}
			</EmptyContent>
		</Empty>
	);
}

export function NoUsersMatchState({ onClear }: { onClear: () => void }) {
	const { t } = useTranslation("admin");

	return (
		<Empty className="border border-dashed bg-muted/20 py-16 ">
			<EmptyHeader>
				<EmptyMedia variant="icon">
					<Users className="size-6" />
				</EmptyMedia>
				<EmptyTitle>{t("users.noMatch.title")}</EmptyTitle>
				<EmptyDescription>{t("users.noMatch.description")}</EmptyDescription>
			</EmptyHeader>
			<EmptyContent>
				<Button size="sm" variant="outline" onClick={onClear}>
					{t("users.noMatch.clear")}
				</Button>
			</EmptyContent>
		</Empty>
	);
}

export function UsersErrorState({ error }: { error?: unknown }) {
	const { t } = useTranslation("admin");

	return (
		<div className="flex flex-col items-center gap-3 rounded-xl border border-destructive/20 bg-destructive/5 py-14 text-center">
			<Users className="size-8 text-destructive/40" />
			<div>
				<p className="text-sm font-medium text-destructive">
					{t("users.errors.loadFailed")}
				</p>
				<p className="mt-0.5 text-xs text-muted-foreground">
					{t("users.errors.tryAgain")}
				</p>
			</div>
			<ReportIssueButton error={error} />
		</div>
	);
}
