import { Link, useRouterState } from "@tanstack/react-router";
import {
	ChevronDown,
	FolderKanban,
	Home,
	Monitor,
	Moon,
	Plus,
	Shield,
	Sun,
	Users,
} from "lucide-react";
import { useState } from "react";

import { Badge } from "@/components/ui/badge";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuGroup,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
	Sidebar,
	SidebarContent,
	SidebarFooter,
	SidebarGroup,
	SidebarGroupContent,
	SidebarGroupLabel,
	SidebarHeader,
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
	SidebarRail,
	SidebarSeparator,
} from "@/components/ui/sidebar";
import { usePermissions } from "@/hooks/use-permissions";
import type { ThemeMode } from "@/hooks/use-theme-mode";
import { useThemeMode } from "@/hooks/use-theme-mode";
import { cn } from "@/lib/utils";

import { UserMenu } from "./user-menu";

// ── Project Switcher ───────────────────────────────────────────────────────────
function ProjectSwitcher() {
	const [open, setOpen] = useState(false);

	return (
		<DropdownMenu open={open} onOpenChange={setOpen}>
			<DropdownMenuTrigger
				className={cn(
					"flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-sm font-medium text-sidebar-foreground/80 transition-all duration-150 select-none cursor-pointer",
					open
						? "bg-primary/10 text-primary"
						: "hover:bg-sidebar-accent/60 hover:text-sidebar-foreground",
				)}
			>
				<div className="flex size-5 shrink-0 items-center justify-center rounded-md bg-primary/15 text-primary">
					<FolderKanban className="size-3" />
				</div>
				<span className="flex-1 truncate text-left">Projects</span>
				<ChevronDown
					className={cn(
						"size-3.5 shrink-0 opacity-40 transition-transform duration-200",
						open && "rotate-180",
					)}
				/>
			</DropdownMenuTrigger>
			<DropdownMenuContent align="start" sideOffset={6} className="w-60">
				<DropdownMenuGroup>
					<DropdownMenuLabel className="text-xs text-muted-foreground pb-1">
						Your Projects
					</DropdownMenuLabel>
				</DropdownMenuGroup>
				<DropdownMenuSeparator />
				<div className="flex flex-col items-center gap-1 px-3 py-4">
					<div className="flex size-8 items-center justify-center rounded-md bg-muted">
						<FolderKanban className="size-4 text-muted-foreground" />
					</div>
					<p className="text-xs text-muted-foreground mt-0.5">
						No projects yet
					</p>
				</div>
				<DropdownMenuSeparator />
				<DropdownMenuItem>
					<Plus className="size-3.5" />
					New project
				</DropdownMenuItem>
			</DropdownMenuContent>
		</DropdownMenu>
	);
}

// ── Nav Item ───────────────────────────────────────────────────────────────────
function NavItem({
	to,
	icon: Icon,
	label,
	badge,
}: {
	to: string;
	icon: React.ComponentType<{ className?: string }>;
	label: string;
	badge?: string;
}) {
	const location = useRouterState({ select: (s) => s.location.pathname });
	const isActive = location === to || location.startsWith(`${to}/`);

	return (
		<SidebarMenuItem>
			<SidebarMenuButton
				isActive={isActive}
				tooltip={label}
				render={<Link to={to} />}
				className={cn(
					"relative transition-all duration-150",
					isActive
						? "bg-primary/10 text-primary font-medium before:absolute before:left-0 before:inset-y-2 before:w-0.75 before:rounded-full before:bg-primary"
						: "hover:bg-sidebar-accent/60",
				)}
			>
				<Icon className="size-4" />
				<span>{label}</span>
				{badge ? (
					<Badge className="ml-auto text-xs" variant="secondary">
						{badge}
					</Badge>
				) : null}
			</SidebarMenuButton>
		</SidebarMenuItem>
	);
}

// ── Theme Switcher ─────────────────────────────────────────────────────────────
const THEME_MODES = [
	{ mode: "light" as ThemeMode, Icon: Sun, label: "Light" },
	{ mode: "dark" as ThemeMode, Icon: Moon, label: "Dark" },
	{ mode: "auto" as ThemeMode, Icon: Monitor, label: "Auto" },
] as const;

function ThemeSwitcher() {
	const { mode, set } = useThemeMode();
	const cycle = () =>
		set(mode === "light" ? "dark" : mode === "dark" ? "auto" : "light");
	const CurrentIcon = mode === "light" ? Sun : mode === "dark" ? Moon : Monitor;

	return (
		<>
			{/* Collapsed: single cycling icon button with tooltip */}
			<SidebarMenu className="hidden group-data-[collapsible=icon]:flex">
				<SidebarMenuItem>
					<SidebarMenuButton
						tooltip={`Theme: ${mode} — click to cycle`}
						onClick={cycle}
					>
						<CurrentIcon className="size-4" />
					</SidebarMenuButton>
				</SidebarMenuItem>
			</SidebarMenu>

			{/* Expanded: segmented 3-way control */}
			<div className="flex items-center justify-between px-2 py-1.5 group-data-[collapsible=icon]:hidden">
				<span className="text-xs font-medium text-sidebar-foreground/50 tracking-wide">
					Theme
				</span>
				<div className="flex items-center gap-0.5 rounded-md border border-sidebar-border bg-sidebar p-0.5">
					{THEME_MODES.map(({ mode: m, Icon, label }) => (
						<button
							key={m}
							type="button"
							onClick={() => set(m)}
							title={label}
							className={cn(
								"flex size-6 items-center justify-center rounded transition-all duration-150",
								mode === m
									? "bg-sidebar-accent text-sidebar-accent-foreground shadow-sm"
									: "text-sidebar-foreground/40 hover:text-sidebar-foreground/70",
							)}
						>
							<Icon className="size-3.5" />
						</button>
					))}
				</div>
			</div>
		</>
	);
}

// ── App Sidebar ────────────────────────────────────────────────────────────────
export function AppSidebar() {
	const { hasPermission } = usePermissions();
	const { resolvedMode } = useThemeMode();

	const canAccessGlobalRoles =
		hasPermission("global_roles.read") || hasPermission("global_roles.write");

	const canAccessUsers =
		hasPermission("users.read") || hasPermission("users.write");

	const showAdminSection = canAccessGlobalRoles || canAccessUsers;

	return (
		<Sidebar collapsible="icon">
			{/* Brand */}
			<SidebarHeader className="gap-2 pb-2">
				<div className="flex items-center gap-2.5 px-2 pt-1">
					<img
						src={
							resolvedMode === "dark" ? "/paca-logo-dark.svg" : "/paca-logo.svg"
						}
						alt="Paca Logo"
						className="size-8 shrink-0"
					/>
					<span className="font-[Syne] font-bold text-[15px] tracking-tight text-sidebar-foreground group-data-[collapsible=icon]:hidden">
						paca
					</span>
				</div>
				<div className="group-data-[collapsible=icon]:hidden">
					<ProjectSwitcher />
				</div>
			</SidebarHeader>

			<SidebarSeparator />

			{/* Main navigation */}
			<SidebarContent>
				<SidebarGroup>
					<SidebarGroupContent>
						<SidebarMenu>
							<NavItem to="/home" icon={Home} label="Home" />
						</SidebarMenu>
					</SidebarGroupContent>
				</SidebarGroup>

				{/* Admin section — only visible when user has at least one admin permission */}
				{showAdminSection ? (
					<>
						<SidebarSeparator />
						<SidebarGroup>
							<SidebarGroupLabel>Administration</SidebarGroupLabel>
							<SidebarGroupContent>
								<SidebarMenu>
									{canAccessGlobalRoles ? (
										<NavItem
											to="/admin/global-roles"
											icon={Shield}
											label="Global Roles"
										/>
									) : null}
									{canAccessUsers ? (
										<NavItem to="/admin/users" icon={Users} label="Users" />
									) : null}
								</SidebarMenu>
							</SidebarGroupContent>
						</SidebarGroup>
					</>
				) : null}
			</SidebarContent>

			{/* Footer: theme toggle + user menu */}
			<SidebarSeparator />
			<SidebarFooter className="gap-1 pb-3">
				<ThemeSwitcher />
				<UserMenu />
			</SidebarFooter>

			<SidebarRail />
		</Sidebar>
	);
}
