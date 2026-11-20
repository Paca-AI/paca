import { Outlet } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { NoPermissionState } from "@/components/shared/no-permission-state";
import { useProjectPermissions } from "@/hooks/use-project-permissions";

/** A project's chat pages. The channels themselves live in the main sidebar
 * (see chat-sidebar-section.tsx), so this only guards access and shows the
 * page, whichever it is: a channel or the channel browser. */
export function ChatLayout({ projectId }: { projectId: string }) {
	const { t } = useTranslation("chat");
	const { hasProjectPermission, isLoading } = useProjectPermissions(projectId);

	if (!isLoading && !hasProjectPermission("channels:read")) {
		return (
			<NoPermissionState
				title={t("noPermission.title")}
				description={t("noPermission.description")}
			/>
		);
	}

	return (
		<div className="flex h-full min-h-0 flex-1">
			<Outlet />
		</div>
	);
}
