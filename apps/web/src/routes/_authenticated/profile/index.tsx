import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { CalendarDays, User } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ChangePasswordCard } from "@/components/profile/ChangePasswordCard";
import { AvatarUpload } from "@/components/shared/avatar-upload";
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
import { Skeleton } from "@/components/ui/skeleton";
import { apiClient } from "@/lib/api-client";
import type { SuccessEnvelope } from "@/lib/api-error";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import type { User as UserType } from "@/lib/auth-api";
import { currentUserQueryOptions } from "@/lib/auth-api";
import { formatDate } from "@/lib/format-date";
import { ExtensionPoint } from "@/lib/plugins/extension-point";

export const Route = createFileRoute("/_authenticated/profile/")({
	component: ProfilePage,
});

async function updateProfile(payload: {
	full_name: string;
	email: string;
}): Promise<UserType> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<UserType>>(
		"/users/me",
		payload,
	);
	return data.data;
}

/** Loose RFC 5322-ish check — the server is the source of truth for validity;
 * this only catches obviously-malformed input before a round trip. */
function isValidEmail(value: string): boolean {
	return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value);
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

function ProfilePage() {
	const { t } = useTranslation("profile");
	const queryClient = useQueryClient();
	const { data: user } = useQuery(currentUserQueryOptions);

	const [editing, setEditing] = useState(false);
	const [fullName, setFullName] = useState(user?.full_name ?? "");
	const [email, setEmail] = useState(user?.email ?? "");
	const [serverError, setServerError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: () =>
			updateProfile({ full_name: fullName.trim(), email: email.trim() }),
		onSuccess: (updated) => {
			queryClient.setQueryData(currentUserQueryOptions.queryKey, updated);
			setEditing(false);
			setServerError(null);
		},
		onError: (err: unknown) => {
			setServerError(
				getApiErrorCode(err) === ApiErrorCode.EmailTaken
					? t("errors.emailTaken")
					: t("errors.updateFailed"),
			);
		},
	});

	if (!user) {
		return (
			<div className="flex flex-col gap-6 p-6 max-w-2xl w-full mx-auto">
				{/* Header skeleton */}
				<div>
					<div className="flex items-center gap-2">
						<Skeleton className="size-5 rounded" />
						<Skeleton className="h-5 w-24" />
					</div>
					<Skeleton className="mt-1.5 h-3.5 w-64" />
				</div>
				<Separator />
				{/* Profile card skeleton */}
				<Card>
					<CardHeader>
						<div className="flex items-center gap-4">
							<Skeleton className="size-14 rounded-xl shrink-0" />
							<div className="space-y-2">
								<Skeleton className="h-5 w-36" />
								<Skeleton className="h-3.5 w-24" />
								<div className="flex items-center gap-2 mt-1">
									<Skeleton className="h-5 w-16 rounded-full" />
									<Skeleton className="h-3.5 w-28" />
								</div>
							</div>
						</div>
					</CardHeader>
					<Separator />
					<CardContent className="pt-5">
						<div className="flex flex-col gap-4">
							<div className="flex flex-col gap-1.5">
								<Skeleton className="h-3.5 w-20" />
								<Skeleton className="h-4 w-40 mt-1" />
							</div>
							<div className="flex flex-col gap-1.5">
								<Skeleton className="h-3.5 w-20" />
								<Skeleton className="h-4 w-32 mt-1" />
							</div>
						</div>
					</CardContent>
					<CardFooter className="border-t pt-4">
						<Skeleton className="h-8 w-24 rounded-md" />
					</CardFooter>
				</Card>
				{/* Change password card skeleton */}
				<Card>
					<CardHeader>
						<Skeleton className="h-5 w-36" />
						<Skeleton className="h-3.5 w-64 mt-1" />
					</CardHeader>
					<CardContent className="space-y-3">
						<div className="flex flex-col gap-1.5">
							<Skeleton className="h-3.5 w-28" />
							<Skeleton className="h-9 w-full rounded-md" />
						</div>
						<div className="flex flex-col gap-1.5">
							<Skeleton className="h-3.5 w-32" />
							<Skeleton className="h-9 w-full rounded-md" />
						</div>
						<div className="flex flex-col gap-1.5">
							<Skeleton className="h-3.5 w-36" />
							<Skeleton className="h-9 w-full rounded-md" />
						</div>
					</CardContent>
					<CardFooter className="border-t pt-4">
						<Skeleton className="h-8 w-32 rounded-md" />
					</CardFooter>
				</Card>
			</div>
		);
	}

	const displayName = user.full_name || user.username;
	const initials = getInitials(displayName);

	const handleEdit = () => {
		setFullName(user.full_name ?? "");
		setEmail(user.email ?? "");
		setServerError(null);
		setEditing(true);
	};

	const emailInvalid =
		editing && email.trim() !== "" && !isValidEmail(email.trim());

	const handleCancel = () => {
		setEditing(false);
		setServerError(null);
	};

	return (
		<div className="flex flex-col gap-6 p-6 max-w-2xl w-full mx-auto">
			{/* Page header */}
			<div>
				<div className="flex items-center gap-2">
					<User className="size-5 text-primary" />
					<h1 className="text-xl font-semibold">{t("page.title")}</h1>
				</div>
				<p className="mt-1 text-sm text-muted-foreground">
					{t("page.subtitle")}
				</p>
			</div>

			<Separator />

			{/* Profile card */}
			<Card>
				<CardHeader>
					<div className="flex items-center gap-4">
						<AvatarUpload
							basePath="/users/me"
							avatarUrl={user.avatar_url}
							fallback={initials}
							className="size-14 rounded-xl"
							fallbackClassName="bg-primary text-primary-foreground text-lg font-bold"
							labels={{
								change: t("avatar.change"),
								remove: t("avatar.remove"),
								uploading: t("avatar.uploading"),
								invalidType: t("avatar.errors.invalidType"),
								tooLarge: t("avatar.errors.tooLarge"),
								uploadFailed: t("avatar.errors.uploadFailed"),
								removeFailed: t("avatar.errors.removeFailed"),
							}}
							onChange={(result) => {
								queryClient.setQueryData(
									currentUserQueryOptions.queryKey,
									(old) => (old ? { ...old, ...result } : old),
								);
							}}
						/>
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
									{t("joinedOn", { date: formatDate(user.created_at) })}
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
							<Label htmlFor="full-name">{t("fields.fullName")}</Label>
							{editing ? (
								<Input
									id="full-name"
									value={fullName}
									onChange={(e) => setFullName(e.target.value)}
									placeholder={t("fields.fullNamePlaceholder")}
									autoFocus
								/>
							) : (
								<p className="text-sm py-1.5">
									{user.full_name || (
										<span className="text-muted-foreground italic">
											{t("fields.notSet")}
										</span>
									)}
								</p>
							)}
						</div>

						{/* Username (read-only) */}
						<div className="flex flex-col gap-1.5">
							<Label>{t("fields.username")}</Label>
							<p className="text-sm py-1.5 text-muted-foreground">
								@{user.username}
							</p>
						</div>

						{/* Email field */}
						<div className="flex flex-col gap-1.5">
							<Label htmlFor="email">{t("fields.email")}</Label>
							{editing ? (
								<>
									<Input
										id="email"
										type="email"
										value={email}
										onChange={(e) => setEmail(e.target.value)}
										placeholder={t("fields.emailPlaceholder")}
										aria-invalid={emailInvalid}
									/>
									{emailInvalid ? (
										<p className="text-xs text-destructive">
											{t("errors.invalidEmail")}
										</p>
									) : null}
								</>
							) : (
								<p className="text-sm py-1.5">
									{user.email || (
										<span className="text-muted-foreground italic">
											{t("fields.notSet")}
										</span>
									)}
								</p>
							)}
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
								disabled={
									mutation.isPending || !fullName.trim() || emailInvalid
								}
							>
								{mutation.isPending
									? t("actions.saving")
									: t("actions.saveChanges")}
							</Button>
							<Button
								size="sm"
								variant="outline"
								onClick={handleCancel}
								disabled={mutation.isPending}
							>
								{t("actions.cancel")}
							</Button>
						</div>
					) : (
						<Button size="sm" variant="outline" onClick={handleEdit}>
							{t("actions.editProfile")}
						</Button>
					)}
				</CardFooter>
			</Card>

			{/* Change Password card */}
			<ChangePasswordCard mustChange={user.must_change_password} />

			{/* Plugin-contributed personal settings (e.g. a plugin's
			    per-event notification preferences) */}
			<ExtensionPoint
				point="user.settings.tab"
				componentProps={{ userId: user.id }}
			/>
		</div>
	);
}
