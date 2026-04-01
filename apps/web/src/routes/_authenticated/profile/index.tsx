import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { CalendarDays, User } from "lucide-react";
import { useState } from "react";
import { ChangePasswordCard } from "@/components/profile/ChangePasswordCard";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardFooter,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { apiClient } from "@/lib/api-client";
import type { SuccessEnvelope } from "@/lib/api-error";
import type { User as UserType } from "@/lib/auth-api";
import { currentUserQueryOptions } from "@/lib/auth-api";

export const Route = createFileRoute("/_authenticated/profile/")({
	component: ProfilePage,
});

async function updateProfile(
	userId: string,
	payload: { full_name: string },
): Promise<UserType> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<UserType>>(
		`/users/${userId}`,
		payload,
	);
	return data.data;
}

function getInitials(name: string): string {
	return name
		.split(" ")
		.filter(Boolean)
		.map((n) => n[0])
		.join("")
		.toUpperCase()
		.slice(0, 2);
}

function formatDate(iso: string) {
	return new Date(iso).toLocaleDateString("en-US", {
		year: "numeric",
		month: "long",
		day: "numeric",
	});
}

function ProfilePage() {
	const queryClient = useQueryClient();
	const { data: user } = useQuery(currentUserQueryOptions);

	const [editing, setEditing] = useState(false);
	const [fullName, setFullName] = useState(user?.full_name ?? "");
	const [serverError, setServerError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: () => {
			if (!user) {
				throw new Error("User is not loaded");
			}
			return updateProfile(user.id, { full_name: fullName.trim() });
		},
		onSuccess: (updated) => {
			queryClient.setQueryData(currentUserQueryOptions.queryKey, updated);
			setEditing(false);
			setServerError(null);
		},
		onError: () => {
			setServerError("Failed to update profile. Please try again.");
		},
	});

	if (!user) return null;

	const displayName = user.full_name || user.username;
	const initials = getInitials(displayName);

	const handleEdit = () => {
		setFullName(user.full_name ?? "");
		setServerError(null);
		setEditing(true);
	};

	const handleCancel = () => {
		setEditing(false);
		setServerError(null);
	};

	return (
		<div className="flex flex-col gap-6 p-6 max-w-2xl">
			{/* Page header */}
			<div>
				<div className="flex items-center gap-2">
					<User className="size-5 text-primary" />
					<h1 className="text-xl font-semibold">My Profile</h1>
				</div>
				<p className="mt-1 text-sm text-muted-foreground">
					View and update your account information.
				</p>
			</div>

			<Separator />

			{/* Profile card */}
			<Card>
				<CardHeader>
					<div className="flex items-center gap-4">
						<Avatar className="size-14 rounded-xl">
							<AvatarFallback className="rounded-xl bg-primary text-primary-foreground text-lg font-bold">
								{initials}
							</AvatarFallback>
						</Avatar>
						<div>
							<CardTitle className="text-lg">{displayName}</CardTitle>
							<CardDescription className="mt-0.5">
								@{user.username}
							</CardDescription>
							<div className="flex items-center gap-2 mt-2">
								<Badge variant="secondary" className="text-xs">
									{user.role}
								</Badge>
								<span className="flex items-center gap-1 text-xs text-muted-foreground">
									<CalendarDays className="size-3" />
									Joined {formatDate(user.created_at)}
								</span>
							</div>
						</div>
					</div>
				</CardHeader>

				<Separator />

				<CardContent className="pt-5">
					<div className="flex flex-col gap-4">
						{/* Full name field */}
						<div className="flex flex-col gap-1.5">
							<Label htmlFor="full-name">Full name</Label>
							{editing ? (
								<Input
									id="full-name"
									value={fullName}
									onChange={(e) => setFullName(e.target.value)}
									placeholder="Enter your full name"
									autoFocus
								/>
							) : (
								<p className="text-sm py-1.5">
									{user.full_name || (
										<span className="text-muted-foreground italic">
											Not set
										</span>
									)}
								</p>
							)}
						</div>

						{/* Username (read-only) */}
						<div className="flex flex-col gap-1.5">
							<Label>Username</Label>
							<p className="text-sm py-1.5 text-muted-foreground">
								@{user.username}
							</p>
						</div>

						{serverError ? (
							<p className="text-sm text-destructive">{serverError}</p>
						) : null}
					</div>
				</CardContent>

				<CardFooter className="border-t pt-4">
					{editing ? (
						<div className="flex gap-2">
							<Button
								size="sm"
								onClick={() => mutation.mutate()}
								disabled={mutation.isPending || !fullName.trim()}
							>
								{mutation.isPending ? "Saving…" : "Save changes"}
							</Button>
							<Button
								size="sm"
								variant="outline"
								onClick={handleCancel}
								disabled={mutation.isPending}
							>
								Cancel
							</Button>
						</div>
					) : (
						<Button size="sm" variant="outline" onClick={handleEdit}>
							Edit profile
						</Button>
					)}
				</CardFooter>
			</Card>

			{/* Change Password card */}
			<ChangePasswordCard mustChange={user.must_change_password} />
		</div>
	);
}
