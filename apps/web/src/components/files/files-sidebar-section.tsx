import { useQuery } from "@tanstack/react-query";
import { Link, useRouterState } from "@tanstack/react-router";
import { ChevronRight, Clock, HardDrive, Star, Trash2 } from "lucide-react";
import { type ComponentType, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	SidebarGroup,
	SidebarGroupContent,
	SidebarGroupLabel,
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
	useSidebar,
} from "@/components/ui/sidebar";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { fileRootsQueryOptions } from "@/lib/files-api";
import { cn } from "@/lib/utils";

// Same inactive treatment as the other sidebar sections.
const INACTIVE =
	"text-sidebar-foreground/80 dark:text-sidebar-foreground/60 hover:bg-sidebar-accent/60 hover:text-sidebar-foreground";

interface Item {
	key: "files" | "recent" | "starred" | "trash";
	icon: ComponentType<{ className?: string }>;
	/** Path below the base; the Files item is the base itself. */
	suffix: string;
}

const ITEMS: readonly Item[] = [
	{ key: "files", icon: HardDrive, suffix: "" },
	{ key: "recent", icon: Clock, suffix: "/recent" },
	{ key: "starred", icon: Star, suffix: "/starred" },
	{ key: "trash", icon: Trash2, suffix: "/trash" },
];

/**
 * The Files section of the sidebar: Files, Recent, Starred and Trash. Inside a
 * project it is scoped to that project; outside one it is the workspace-wide
 * Files, listed only for people who can read some files.
 */
export function FilesSidebarSection({ projectId }: { projectId?: string }) {
	const { t } = useTranslation("files");
	const location = useRouterState({ select: (s) => s.location.pathname });
	const { state, isMobile } = useSidebar();
	const { hasProjectPermission } = useProjectPermissions(projectId ?? "");
	const roots = useQuery({ ...fileRootsQueryOptions, enabled: !projectId });

	const storageKey = `paca:sidebar-files-collapsed:${projectId ?? "global"}`;
	const [collapsed, setCollapsed] = useState(() => {
		try {
			return localStorage.getItem(storageKey) === "true";
		} catch {
			return false;
		}
	});
	const toggle = () =>
		setCollapsed((prev) => {
			try {
				localStorage.setItem(storageKey, String(!prev));
			} catch {
				/* ignore */
			}
			return !prev;
		});

	const canRead = projectId
		? hasProjectPermission("files:read")
		: (roots.data?.length ?? 0) > 0;
	if (!canRead) return null;
	// A section header with nothing to click is noise on the icon rail.
	if (state === "collapsed" && !isMobile) return null;

	const base = projectId ? `/projects/${projectId}/files` : "/files";

	return (
		<SidebarGroup>
			<SidebarGroupLabel
				render={<button type="button" />}
				aria-expanded={!collapsed}
				className="flex w-full cursor-pointer items-center justify-between text-left transition-colors hover:text-sidebar-foreground"
				onClick={toggle}
			>
				<span>{t("title")}</span>
				<ChevronRight
					className={cn(
						"size-3.5 text-sidebar-foreground/60 transition-transform duration-200",
						!collapsed && "rotate-90",
					)}
				/>
			</SidebarGroupLabel>
			{!collapsed && (
				<SidebarGroupContent>
					<SidebarMenu>
						{ITEMS.map(({ key, icon: Icon, suffix }) => {
							const to = `${base}${suffix}`;
							const active = location === to;
							return (
								<SidebarMenuItem key={key}>
									<SidebarMenuButton
										isActive={active}
										tooltip={t(`nav.${key}`)}
										render={<Link to={to} />}
										className={cn(
											"relative transition-all duration-150",
											active
												? "bg-primary/10 font-medium text-primary before:absolute before:inset-y-2 before:left-0 before:w-0.75 before:rounded-full before:bg-primary"
												: INACTIVE,
										)}
									>
										<Icon className="size-4" />
										<span>{t(`nav.${key}`)}</span>
									</SidebarMenuButton>
								</SidebarMenuItem>
							);
						})}
					</SidebarMenu>
				</SidebarGroupContent>
			)}
		</SidebarGroup>
	);
}
