import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, Eye, EyeOff, KeyRound, UserRound } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogClose,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import {
	createUser,
	globalRolesQueryOptions,
	type User,
	updateUser,
	usersQueryOptions,
} from "@/lib/admin-api";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { generatePassword } from "@/lib/generate-password";

interface UserFormDialogProps {
	user?: User;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

export function UserFormDialog({
	user,
	open,
	onOpenChange,
}: UserFormDialogProps) {
	const queryClient = useQueryClient();
	const isEdit = !!user;

	const [username, setUsername] = useState(user?.username ?? "");
	const [fullName, setFullName] = useState(user?.full_name ?? "");
	const [role, setRole] = useState(user?.role ?? "");
	const [error, setError] = useState<string | null>(null);
	const [usernameError, setUsernameError] = useState<string | null>(null);

	// Created-state: holds the generated password to display after creation
	const [createdPassword, setCreatedPassword] = useState<string | null>(null);
	const [showPassword, setShowPassword] = useState(false);
	const [copied, setCopied] = useState(false);

	const { data: roles = [] } = useQuery(globalRolesQueryOptions);

	const reset = () => {
		setUsername(user?.username ?? "");
		setFullName(user?.full_name ?? "");
		setRole(user?.role ?? "");
		setError(null);
		setUsernameError(null);
		setCreatedPassword(null);
		setShowPassword(false);
		setCopied(false);
	};

	const handleOpenChange = (next: boolean) => {
		if (!next) reset();
		onOpenChange(next);
	};

	const handleCopy = () => {
		if (!createdPassword) return;
		void navigator.clipboard.writeText(createdPassword).then(() => {
			setCopied(true);
			setTimeout(() => setCopied(false), 2000);
		});
	};

	const mutation = useMutation({
		mutationFn: async () => {
			if (!fullName.trim()) throw new Error("Full name is required.");

			if (isEdit && user) {
				return updateUser(user.id, {
					full_name: fullName.trim(),
					role: role || undefined,
				});
			}

			if (!username.trim()) throw new Error("Username is required.");

			const password = generatePassword();
			await createUser({
				username: username.trim(),
				password,
				full_name: fullName.trim(),
				role: role || undefined,
			});
			return password;
		},
		onSuccess: (result) => {
			void queryClient.invalidateQueries({
				queryKey: usersQueryOptions().queryKey.slice(0, 2),
			});
			if (isEdit) {
				onOpenChange(false);
				reset();
			} else {
				// Show the generated password instead of closing
				setCreatedPassword(result as string);
			}
		},
		onError: (err: unknown) => {
			setUsernameError(null);
			const code = getApiErrorCode(err);
			if (code === ApiErrorCode.UsernameTaken) {
				setUsernameError("This username is already taken.");
				return;
			}
			const messages: Partial<Record<string, string>> = {
				[ApiErrorCode.UserNotFound]: "User not found. They may have already been deleted.",
				[ApiErrorCode.Forbidden]: "You don't have permission to perform this action.",
				[ApiErrorCode.InternalError]: "Something went wrong on the server. Please try again.",
			};
			const message = (err instanceof Error ? err.message : null);
			setError((code && messages[code]) ?? message ?? "Something went wrong.");
		},
	});

	// ── Post-creation success screen ────────────────────────────────────────
	if (createdPassword) {
		return (
			<Dialog open={open} onOpenChange={handleOpenChange}>
				<DialogContent className="sm:max-w-md">
					<DialogHeader>
						<div className="flex items-center gap-2.5">
							<div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-emerald-100 text-emerald-600 dark:bg-emerald-950/50 dark:text-emerald-400">
								<KeyRound className="size-4" />
							</div>
							<DialogTitle className="text-base">User created</DialogTitle>
						</div>
						<DialogDescription className="mt-2">
							<strong className="text-foreground">{username}</strong> has been
							created. Share the temporary password below — the user will be
							asked to change it on first login.
						</DialogDescription>
					</DialogHeader>

					<div className="flex flex-col gap-3 py-1">
						<Label className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
							Temporary password
						</Label>
						<div className="flex items-center gap-2">
							<div className="relative flex-1">
								<Input
									readOnly
									type={showPassword ? "text" : "password"}
									value={createdPassword}
									className="font-mono pr-10 select-all"
								/>
								<button
									type="button"
									onClick={() => setShowPassword((v) => !v)}
									className="absolute inset-y-0 right-2 flex items-center text-muted-foreground hover:text-foreground transition-colors"
									aria-label={showPassword ? "Hide password" : "Show password"}
								>
									{showPassword ? (
										<EyeOff className="size-4" />
									) : (
										<Eye className="size-4" />
									)}
								</button>
							</div>
							<Button
								variant="outline"
								size="icon"
								onClick={handleCopy}
								aria-label="Copy password"
							>
								{copied ? (
									<Check className="size-4 text-emerald-500" />
								) : (
									<Copy className="size-4" />
								)}
							</Button>
						</div>
						<p className="text-xs text-muted-foreground">
							This password will not be shown again. Make sure to copy it now.
						</p>
					</div>

					<DialogFooter>
						<Button onClick={() => handleOpenChange(false)}>Done</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		);
	}

	// ── Create / Edit form ───────────────────────────────────────────────────
	return (
		<Dialog open={open} onOpenChange={handleOpenChange}>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<div className="flex items-center gap-2.5">
						<div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
							<UserRound className="size-4" />
						</div>
						<DialogTitle className="text-base">
							{isEdit ? "Edit User" : "Create User"}
						</DialogTitle>
					</div>
					<DialogDescription className="mt-2">
						{isEdit
							? "Update the user's display name and role assignment."
							: "A secure temporary password will be generated automatically. The user will be required to change it on first login."}
					</DialogDescription>
				</DialogHeader>

				<div className="flex flex-col gap-4 py-1">
					{!isEdit ? (
						<div className="flex flex-col gap-1.5">
							<Label
								htmlFor="user-username"
								className="text-xs font-semibold uppercase tracking-wide text-muted-foreground"
							>
								Username
							</Label>
							<Input
								id="user-username"
								placeholder="e.g. john.doe"
								value={username}
								onChange={(e) => {
									setUsername(e.target.value);
									if (usernameError) setUsernameError(null);
								}}
								autoComplete="off"
								className={`font-mono${usernameError ? " border-destructive focus-visible:ring-destructive" : ""}`}
								aria-describedby={usernameError ? "username-error" : undefined}
							/>
							{usernameError ? (
								<p id="username-error" className="text-xs text-destructive">{usernameError}</p>
							) : null}
						</div>
					) : null}

					<div className="flex flex-col gap-1.5">
						<Label
							htmlFor="user-fullname"
							className="text-xs font-semibold uppercase tracking-wide text-muted-foreground"
						>
							Full Name
						</Label>
						<Input
							id="user-fullname"
							placeholder="e.g. John Doe"
							value={fullName}
							onChange={(e) => setFullName(e.target.value)}
							autoComplete="off"
						/>
					</div>

					<div className="flex flex-col gap-1.5">
						<Label
							htmlFor="user-role"
							className="text-xs font-semibold uppercase tracking-wide text-muted-foreground"
						>
							Role{" "}
							<span className="normal-case font-normal text-muted-foreground/70">
								(optional — defaults to USER)
							</span>
						</Label>
						<Select value={role} onValueChange={(v) => setRole(v ?? "")}>
							<SelectTrigger id="user-role" className="w-full">
								<SelectValue placeholder="Select a role…" />
							</SelectTrigger>
							<SelectContent>
								{roles.map((r) => (
									<SelectItem key={r.id} value={r.name}>
										{r.name}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					</div>

					{error ? (
						<div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">
							<span className="shrink-0">⚠</span>
							<span>{error}</span>
						</div>
					) : null}
				</div>

				<DialogFooter>
					<DialogClose render={<Button variant="outline" />}>Cancel</DialogClose>
					<Button
						onClick={() => mutation.mutate()}
						disabled={mutation.isPending}
					>
						{mutation.isPending
							? isEdit
								? "Saving…"
								: "Creating…"
							: isEdit
								? "Save changes"
								: "Create user"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
