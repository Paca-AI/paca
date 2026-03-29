import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ChevronsUpDown, LogOut, User } from "lucide-react";
import { useState } from "react";

import { Avatar, AvatarFallback } from "@/components/ui/avatar";
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
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
} from "@/components/ui/sidebar";
import { currentUserQueryOptions, logout } from "@/lib/auth-api";

function getInitials(name: string): string {
	return name
		.split(" ")
		.filter(Boolean)
		.map((n) => n[0])
		.join("")
		.toUpperCase()
		.slice(0, 2);
}

export function UserMenu() {
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const { data: user } = useQuery(currentUserQueryOptions);
	const [isLoggingOut, setIsLoggingOut] = useState(false);

	if (!user) return null;

	const displayName = user.full_name || user.username;
	const initials = getInitials(displayName);

	const handleLogout = async () => {
		setIsLoggingOut(true);
		try {
			await logout();
			await queryClient.cancelQueries({
				queryKey: currentUserQueryOptions.queryKey,
			});
			queryClient.removeQueries({
				queryKey: currentUserQueryOptions.queryKey,
				exact: true,
			});
			await navigate({ to: "/", replace: true });
		} finally {
			setIsLoggingOut(false);
		}
	};

	return (
		<SidebarMenu>
			<SidebarMenuItem>
				<DropdownMenu>
					<DropdownMenuTrigger
						className="w-full"
						render={
							<SidebarMenuButton
								size="lg"
								className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
							/>
						}
					>
						<Avatar size="sm" className="rounded-lg">
							<AvatarFallback className="rounded-lg bg-primary text-primary-foreground text-xs font-semibold">
								{initials}
							</AvatarFallback>
						</Avatar>
						<div className="grid flex-1 text-left text-sm leading-tight">
							<span className="truncate font-semibold">{displayName}</span>
							<span className="truncate text-xs text-muted-foreground capitalize">
								{user.role.toLowerCase()}
							</span>
						</div>
						<ChevronsUpDown className="ml-auto size-4 shrink-0 opacity-50" />
					</DropdownMenuTrigger>
					<DropdownMenuContent
						side="top"
						sideOffset={4}
						align="end"
						className="w-56"
					>
						<DropdownMenuGroup>
							<DropdownMenuLabel className="font-normal">
								<div className="flex flex-col gap-0.5">
									<span className="font-medium text-sm">{displayName}</span>
									<span className="text-xs text-muted-foreground">
										@{user.username}
									</span>
								</div>
							</DropdownMenuLabel>
						</DropdownMenuGroup>
						<DropdownMenuSeparator />
						<DropdownMenuItem onClick={() => void navigate({ to: "/profile" })}>
							<User className="size-4" />
							My Profile
						</DropdownMenuItem>
						<DropdownMenuSeparator />
						<DropdownMenuItem
							variant="destructive"
							onClick={() => void handleLogout()}
							disabled={isLoggingOut}
						>
							<LogOut className="size-4" />
							{isLoggingOut ? "Logging out…" : "Log out"}
						</DropdownMenuItem>
					</DropdownMenuContent>
				</DropdownMenu>
			</SidebarMenuItem>
		</SidebarMenu>
	);
}
