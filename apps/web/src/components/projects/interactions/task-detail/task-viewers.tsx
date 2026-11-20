import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { EntityAvatar } from "@/components/shared/entity-avatar";
import { useTaskPresence } from "@/hooks/use-live-presence";
import { currentUserQueryOptions } from "@/lib/auth-api";
import { useMemberDirectory } from "@/lib/member-directory";
import { memberAvatar } from "@/lib/provider-logos";

const MAX_SHOWN = 3;

/** Avatars of the other people looking at this task right now. Renders
 *  nothing while you are the only viewer. */
export function TaskViewers({
	projectId,
	taskId,
}: {
	projectId?: string;
	taskId: string;
}) {
	const { t } = useTranslation("projects");
	const { viewers } = useTaskPresence(projectId, taskId);
	const { data: me } = useQuery(currentUserQueryOptions);
	const getMember = useMemberDirectory(projectId ?? "");

	const others = useMemo(
		() => viewers.filter((id) => id !== me?.id),
		[viewers, me?.id],
	);
	if (!projectId || others.length === 0) return null;

	const members = others.map((id) => ({ id, member: getMember(id) }));
	const names = members
		.map(({ member }) => member?.full_name || member?.username)
		.filter(Boolean)
		.join(", ");
	const shown = members.slice(0, MAX_SHOWN);
	const extra = members.length - shown.length;

	return (
		<div
			data-testid="task-viewers"
			role="img"
			aria-label={t("taskDetail.header.viewingNow", { names })}
			title={t("taskDetail.header.viewingNow", { names })}
			className="mr-1 flex items-center -space-x-1.5"
		>
			{shown.map(({ id, member }) => (
				<EntityAvatar
					key={id}
					{...(member ? memberAvatar(member) : { name: "" })}
					size="xs"
					className="ring-2 ring-background"
				/>
			))}
			{extra > 0 && (
				<span className="z-10 ml-1 pl-2 text-xs font-medium text-muted-foreground">
					+{extra}
				</span>
			)}
		</div>
	);
}
