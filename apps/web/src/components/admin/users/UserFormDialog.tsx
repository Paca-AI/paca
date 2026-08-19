import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, Eye, EyeOff, KeyRound, UserRound } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

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
import { validateUsername } from "@/lib/auth-validation";
import { generatePassword } from "@/lib/generate-password";

/** Loose RFC 5322-ish check — the server is the source of truth for validity;
 * this only catches obviously-malformed input before a round trip. */
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

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
	const { t } = useTranslation("admin");
	const { t: tCommon } = useTranslation("common");
	const queryClient = useQueryClient();
	const isEdit = !!user;

	const [username, setUsername] = useState(user?.username ?? "");
	const [fullName, setFullName] = useState(user?.full_name ?? "");
	const [email, setEmail] = useState(user?.email ?? "");
	const [role, setRole] = useState(user?.role ?? "");
	const [error, setError] = useState<string | null>(null);
	const [usernameError, setUsernameError] = useState<string | null>(null);
	const [emailError, setEmailError] = useState<string | null>(null);

	// Created-state: holds the generated password to display after creation
	const [createdPassword, setCreatedPassword] = useState<string | null>(null);
	const [showPassword, setShowPassword] = useState(false);
	const [copied, setCopied] = useState(false);

	const { data: roles = [] } = useQuery(globalRolesQueryOptions);

	const reset = () => {
		setUsername(user?.username ?? "");
		setFullName(user?.full_name ?? "");
		setEmail(user?.email ?? "");
		setRole(user?.role ?? "");
		setError(null);
		setUsernameError(null);
		setEmailError(null);
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
			if (!fullName.trim())
				throw new Error(t("users.formDialog.errors.fullNameRequired"));
			const trimmedEmail = email.trim();
			if (trimmedEmail && !EMAIL_PATTERN.test(trimmedEmail)) {
				throw new Error(t("users.formDialog.errors.invalidEmail"));
			}

			if (isEdit && user) {
				return updateUser(user.id, {
					full_name: fullName.trim(),
					email: trimmedEmail || undefined,
					role: role || undefined,
				});
			}

			const usernameError = validateUsername(username, tCommon);
			if (usernameError) throw new Error(usernameError);

			const password = generatePassword();
			await createUser({
				username: username.trim(),
				password,
				full_name: fullName.trim(),
				email: trimmedEmail || undefined,
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
			setEmailError(null);
			const code = getApiErrorCode(err);
			if (code === ApiErrorCode.UsernameTaken) {
				setUsernameError(t("users.formDialog.errors.usernameTaken"));
				return;
			}
			if (code === ApiErrorCode.EmailTaken) {
				setEmailError(t("users.formDialog.errors.emailTaken"));
				return;
			}
			if (
				err instanceof Error &&
				err.message.toLowerCase().includes("username")
			) {
				setUsernameError(err.message);
				return;
			}
			const messages: Partial<Record<string, string>> = {
				[ApiErrorCode.UserNotFound]: t("users.formDialog.errors.userNotFound"),
				[ApiErrorCode.Forbidden]: t("users.formDialog.errors.forbidden"),
				[ApiErrorCode.InternalError]: t(
					"users.formDialog.errors.internalError",
				),
			};
			const message = err instanceof Error ? err.message : null;
			setError(
				(code && messages[code]) ??
					message ??
					t("users.formDialog.errors.generic"),
			);
		},
	});

	// ── Post-creation success screen ────────────────────────────────────────
	if (createdPassword) {
		return (
			<Dialog open={open} onOpenChange={handleOpenChange}>
				<DialogContent className="sm:max-w-md">
					<DialogHeader>
						<div className="flex items-center gap-2.5">
							<div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
								<KeyRound className="size-4" />
							</div>
							<DialogTitle className="text-base">
								{t("users.formDialog.createdTitle")}
							</DialogTitle>
						</div>
						<DialogDescription className="mt-2">
							<strong className="text-foreground">{username}</strong>{" "}
							{t("users.formDialog.createdDescriptionSuffix")}
						</DialogDescription>
					</DialogHeader>

					<div className="flex flex-col gap-3 py-1">
						<Label className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
							{t("users.formDialog.temporaryPasswordLabel")}
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
									aria-label={
										showPassword
											? t("users.formDialog.hidePassword")
											: t("users.formDialog.showPassword")
									}
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
								aria-label={t("users.formDialog.copyPassword")}
							>
								{copied ? (
									<Check className="size-4 text-emerald-500" />
								) : (
									<Copy className="size-4" />
								)}
							</Button>
						</div>
						<p className="text-xs text-muted-foreground">
							{t("users.formDialog.passwordNotShownAgain")}
						</p>
					</div>

					<DialogFooter>
						<Button onClick={() => handleOpenChange(false)}>
							{t("users.formDialog.done")}
						</Button>
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
							{isEdit
								? t("users.formDialog.editTitle")
								: t("users.formDialog.createTitle")}
						</DialogTitle>
					</div>
					<DialogDescription className="mt-2">
						{isEdit
							? t("users.formDialog.editDescription")
							: t("users.formDialog.createDescription")}
					</DialogDescription>
				</DialogHeader>

				<div className="flex flex-col gap-4 py-1">
					{!isEdit ? (
						<div className="flex flex-col gap-1.5">
							<Label
								htmlFor="user-username"
								className="text-xs font-semibold uppercase tracking-wide text-muted-foreground"
							>
								{t("users.formDialog.usernameLabel")}
							</Label>
							<Input
								id="user-username"
								placeholder={t("users.formDialog.usernamePlaceholder")}
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
								<p id="username-error" className="text-xs text-destructive">
									{usernameError}
								</p>
							) : null}
						</div>
					) : null}

					<div className="flex flex-col gap-1.5">
						<Label
							htmlFor="user-fullname"
							className="text-xs font-semibold uppercase tracking-wide text-muted-foreground"
						>
							{t("users.formDialog.fullNameLabel")}
						</Label>
						<Input
							id="user-fullname"
							placeholder={t("users.formDialog.fullNamePlaceholder")}
							value={fullName}
							onChange={(e) => setFullName(e.target.value)}
							autoComplete="off"
						/>
					</div>

					<div className="flex flex-col gap-1.5">
						<Label
							htmlFor="user-email"
							className="text-xs font-semibold uppercase tracking-wide text-muted-foreground"
						>
							{t("users.formDialog.emailLabel")}{" "}
							<span className="normal-case font-normal text-muted-foreground/70">
								{t("users.formDialog.emailOptionalHint")}
							</span>
						</Label>
						<Input
							id="user-email"
							type="email"
							placeholder={t("users.formDialog.emailPlaceholder")}
							value={email}
							onChange={(e) => {
								setEmail(e.target.value);
								if (emailError) setEmailError(null);
							}}
							autoComplete="off"
							className={
								emailError
									? "border-destructive focus-visible:ring-destructive"
									: undefined
							}
							aria-describedby={emailError ? "email-error" : undefined}
						/>
						{emailError ? (
							<p id="email-error" className="text-xs text-destructive">
								{emailError}
							</p>
						) : null}
					</div>

					<div className="flex flex-col gap-1.5">
						<Label
							htmlFor="user-role"
							className="text-xs font-semibold uppercase tracking-wide text-muted-foreground"
						>
							{t("users.formDialog.roleLabel")}{" "}
							<span className="normal-case font-normal text-muted-foreground/70">
								{t("users.formDialog.roleOptionalHint")}
							</span>
						</Label>
						<Select value={role} onValueChange={(v) => setRole(v ?? "")}>
							<SelectTrigger id="user-role" className="w-full">
								<SelectValue
									placeholder={t("users.formDialog.rolePlaceholder")}
								/>
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
					<DialogClose render={<Button variant="outline" />}>
						{t("users.formDialog.cancel")}
					</DialogClose>
					<Button
						onClick={() => mutation.mutate()}
						disabled={mutation.isPending}
					>
						{mutation.isPending
							? isEdit
								? t("users.formDialog.saving")
								: t("users.formDialog.creating")
							: isEdit
								? t("users.formDialog.saveChanges")
								: t("users.formDialog.createUser")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
