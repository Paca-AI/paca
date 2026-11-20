import { useTranslation } from "react-i18next";
import { EntityAvatar } from "@/components/shared/entity-avatar";
import { useLivePresence } from "@/hooks/use-live-presence";
import type { LiveKind } from "@/lib/live-presence";
import { useMemberDirectory } from "@/lib/member-directory";
import { memberAvatar } from "@/lib/provider-logos";

const MAX_SHOWN = 3;

/** Avatars of the other people looking at this task, doc or channel right
 *  now. Renders nothing while you are the only viewer. */
export function PresenceViewers({
	kind,
	projectId,
	id,
	testId = "presence-viewers",
}: {
	kind: LiveKind;
	/** "" for a direct message. */
	projectId: string | undefined;
	id: string;
	testId?: string;
}) {
	const { t } = useTranslation("projects");
	const { otherViewers, nameOf } = useLivePresence(kind, projectId, id);
	const getMember = useMemberDirectory(projectId ?? "");
	if (otherViewers.length === 0) return null;

	const people = otherViewers.map((who) => ({
		id: who,
		member: projectId ? getMember(who) : undefined,
		name: nameOf(who),
	}));
	const names = people
		.map((p) => p.name)
		.filter(Boolean)
		.join(", ");
	const shown = people.slice(0, MAX_SHOWN);
	const extra = people.length - shown.length;
	const label = t("taskDetail.header.viewingNow", { names });

	return (
		<div
			data-testid={testId}
			role="img"
			aria-label={label}
			title={label}
			className="mr-1 flex items-center -space-x-1.5"
		>
			{shown.map((p) => (
				<EntityAvatar
					key={p.id}
					{...(p.member ? memberAvatar(p.member) : { name: p.name ?? "" })}
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
