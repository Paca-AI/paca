import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
	Check,
	Copy,
	Eye,
	EyeOff,
	KeyRound,
	ShieldCheck,
	UserRound,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
	isFullAccessRole,
	RolePicker,
} from "@/components/admin/global-roles/role-picker";
import { UserRoleResult } from "@/components/admin/users/UserRoleResult";
import { InlineNotice } from "@/components/shared/inline-notice";
import { StepIndicator } from "@/components/shared/step-indicator";
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
import { useCanAssignGlobalRole } from "@/hooks/use-can-assign-global-role";
import {
	assignUserGlobalRole,
	createUser,
	type GlobalRole,
	type User,
	updateUser,
	usersQueryOptions,
} from "@/lib/admin-api";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { validateUsername } from "@/lib/auth-validation";
import { generatePassword } from "@/lib/generate-password";

/** The role every new account starts with (see the API's user creation). */
const DEFAULT_ROLE = "USER";

/** Loose RFC 5322-ish check — the server is the source of truth for validity;
 * this only catches obviously-malformed input before a round trip. */
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/** The steps of creating an account. Editing one is a single "details" step. */
type Phase = "details" | "role" | "password";

interface ProfileProblem {
	field: "username" | "fullName" | "email";
	message: string;
}

interface SaveResult {
	user: User;
	/** Set only when an account was created. Shown once, on the last step. */
	password?: string;
	/** The non-default role that was picked for the new account, if any. */
	role?: GlobalRole | null;
	/** Whether that role was assigned (it is a separate request and can fail). */
	roleAssigned?: boolean;
}

interface UserFormDialogProps {
	user?: User;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

/**
 * Creates a user or edits their profile (name and email).
 *
 * Creating is a short wizard: 1 Details → 2 Role → 3 Password. Nothing is
 * created until the last action before the password, so backing out at any
 * point leaves nothing behind, and the one-time password is the very last
 * thing shown. The role step exists only for someone who may assign roles
 * (`global_roles.assign`, its own permission); without it the wizard is
 * Details → Password and the account starts as USER. Editing never touches the
 * role: that is UserRoleDialog, opened from the users table.
 */
export function UserFormDialog({
	user,
	open,
	onOpenChange,
}: UserFormDialogProps) {
	const { t } = useTranslation("admin");
	const { t: tCommon } = useTranslation("common");
	const queryClient = useQueryClient();
	const isEdit = !!user;
	const canAssignRole = useCanAssignGlobalRole();
	const hasRoleStep = !isEdit && canAssignRole;
	const totalSteps = hasRoleStep ? 3 : 2;

	const [phase, setPhase] = useState<Phase>("details");
	const [username, setUsername] = useState(user?.username ?? "");
	const [fullName, setFullName] = useState(user?.full_name ?? "");
	const [email, setEmail] = useState(user?.email ?? "");
	const [selectedRole, setSelectedRole] = useState<GlobalRole | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [usernameError, setUsernameError] = useState<string | null>(null);
	const [emailError, setEmailError] = useState<string | null>(null);

	// The new account and its generated password, shown once on the last step
	const [created, setCreated] = useState<SaveResult | null>(null);
	const [showPassword, setShowPassword] = useState(false);
	const [copied, setCopied] = useState(false);

	const stepNumber =
		phase === "details" ? 1 : phase === "role" ? 2 : totalSteps;

	const reset = () => {
		setPhase("details");
		setUsername(user?.username ?? "");
		setFullName(user?.full_name ?? "");
		setEmail(user?.email ?? "");
		setSelectedRole(null);
		setError(null);
		setUsernameError(null);
		setEmailError(null);
		setCreated(null);
		setShowPassword(false);
		setCopied(false);
	};

	const handleOpenChange = (next: boolean) => {
		if (!next) reset();
		onOpenChange(next);
	};

	const handleCopy = () => {
		if (!created?.password) return;
		void navigator.clipboard.writeText(created.password).then(() => {
			setCopied(true);
			setTimeout(() => setCopied(false), 2000);
		});
	};

	const validateProfile = (): ProfileProblem | null => {
		if (!fullName.trim()) {
			return {
				field: "fullName",
				message: t("users.formDialog.errors.fullNameRequired"),
			};
		}
		const trimmedEmail = email.trim();
		if (trimmedEmail && !EMAIL_PATTERN.test(trimmedEmail)) {
			return {
				field: "email",
				message: t("users.formDialog.errors.invalidEmail"),
			};
		}
		if (!isEdit) {
			const message = validateUsername(username, tCommon);
			if (message) return { field: "username", message };
		}
		return null;
	};

	const showProblem = ({ field, message }: ProfileProblem) => {
		setError(null);
		setUsernameError(null);
		setEmailError(null);
		if (field === "username") setUsernameError(message);
		else if (field === "email") setEmailError(message);
		else setError(message);
	};

	const mutation = useMutation({
		mutationFn: async (): Promise<SaveResult> => {
			if (isEdit && user) {
				const updated = await updateUser(user.id, {
					full_name: fullName.trim(),
					email: email.trim() || undefined,
				});
				return { user: updated };
			}

			const password = generatePassword();
			const createdUser = await createUser({
				username: username.trim(),
				password,
				full_name: fullName.trim(),
				email: email.trim() || undefined,
			});

			// A role other than the default is a second request (and permission).
			// The account exists by now, so a failure here must not throw: the
			// generated password is shown only once and would be lost with it.
			// The last step reports it, with a retry.
			const role =
				selectedRole && selectedRole.name !== DEFAULT_ROLE
					? selectedRole
					: null;
			let roleAssigned = false;
			if (role) {
				try {
					await assignUserGlobalRole(createdUser.id, role.id);
					roleAssigned = true;
				} catch {
					roleAssigned = false;
				}
			}
			return { user: createdUser, password, role, roleAssigned };
		},
		onSuccess: (result) => {
			void queryClient.invalidateQueries({
				queryKey: usersQueryOptions().queryKey.slice(0, 2),
			});
			if (result.password) {
				setCreated(result);
				setPhase("password");
			} else {
				onOpenChange(false);
				reset();
			}
		},
		onError: (err: unknown) => {
			setError(null);
			setUsernameError(null);
			setEmailError(null);
			const code = getApiErrorCode(err);
			// These two belong to fields of the first step, so go back to them.
			if (code === ApiErrorCode.UsernameTaken) {
				setPhase("details");
				setUsernameError(t("users.formDialog.errors.usernameTaken"));
				return;
			}
			if (code === ApiErrorCode.EmailTaken) {
				setPhase("details");
				setEmailError(t("users.formDialog.errors.emailTaken"));
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

	const handleContinue = () => {
		const problem = validateProfile();
		if (problem) {
			showProblem(problem);
			return;
		}
		setError(null);
		setPhase("role");
	};

	const handleSubmit = () => {
		const problem = validateProfile();
		if (problem) {
			setPhase("details");
			showProblem(problem);
			return;
		}
		setError(null);
		mutation.mutate();
	};

	// ── Last step: the one-time password ─────────────────────────────────────
	if (phase === "password" && created?.password) {
		return (
			<Dialog open={open} onOpenChange={handleOpenChange}>
				<DialogContent className="sm:max-w-md">
					<DialogHeader>
						<div className="flex items-center justify-between gap-3 pr-7">
							<div className="flex items-center gap-2.5">
								<div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
									<KeyRound className="size-4" />
								</div>
								<DialogTitle className="text-base">
									{t("users.formDialog.createdTitle")}
								</DialogTitle>
							</div>
							<StepIndicator
								step={stepNumber}
								total={totalSteps}
								label={t("users.formDialog.stepIndicator", {
									step: stepNumber,
									total: totalSteps,
								})}
							/>
						</div>
						<DialogDescription className="mt-2">
							<strong className="text-foreground">{username.trim()}</strong>{" "}
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
									value={created.password}
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

					{hasRoleStep ? (
						<UserRoleResult
							user={created.user}
							role={created.role ?? null}
							assigned={created.roleAssigned ?? false}
							onAssigned={() =>
								setCreated((prev) =>
									prev ? { ...prev, roleAssigned: true } : prev,
								)
							}
						/>
					) : null}

					<DialogFooter>
						<Button onClick={() => handleOpenChange(false)}>
							{t("users.formDialog.done")}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		);
	}

	// ── Create (details, role) / Edit ────────────────────────────────────────
	return (
		<Dialog open={open} onOpenChange={handleOpenChange}>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<div className="flex items-center justify-between gap-3 pr-7">
						<div className="flex items-center gap-2.5">
							<div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
								{phase === "role" ? (
									<ShieldCheck className="size-4" />
								) : (
									<UserRound className="size-4" />
								)}
							</div>
							<DialogTitle className="text-base">
								{isEdit
									? t("users.formDialog.editTitle")
									: t("users.formDialog.createTitle")}
							</DialogTitle>
						</div>
						{isEdit ? null : (
							<StepIndicator
								step={stepNumber}
								total={totalSteps}
								label={t("users.formDialog.stepIndicator", {
									step: stepNumber,
									total: totalSteps,
								})}
							/>
						)}
					</div>
					<DialogDescription className="mt-2">
						{phase === "role"
							? t("users.formDialog.roleStepDescription", {
									username: username.trim(),
								})
							: isEdit
								? t("users.formDialog.editDescription")
								: t("users.formDialog.createDescription")}
					</DialogDescription>
				</DialogHeader>

				{phase === "role" ? (
					<div className="flex flex-col gap-3 py-1">
						<RolePicker
							label={t("users.formDialog.roleStep.title")}
							value={selectedRole?.id ?? null}
							onChange={(role) => {
								setSelectedRole(role);
								setError(null);
							}}
							currentRoleName={DEFAULT_ROLE}
							currentBadge={t("rolePicker.default")}
							disabled={mutation.isPending}
						/>
						{selectedRole && isFullAccessRole(selectedRole) ? (
							<InlineNotice tone="warning">
								{t("users.roleDialog.fullAccessWarning")}
							</InlineNotice>
						) : null}
						{error ? <InlineNotice tone="error">{error}</InlineNotice> : null}
					</div>
				) : (
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
									aria-describedby={
										usernameError ? "username-error" : undefined
									}
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

						{error ? <InlineNotice tone="error">{error}</InlineNotice> : null}
					</div>
				)}

				<DialogFooter>
					{phase === "role" ? (
						<Button
							variant="outline"
							onClick={() => setPhase("details")}
							disabled={mutation.isPending}
						>
							{t("users.formDialog.back")}
						</Button>
					) : (
						<DialogClose render={<Button variant="outline" />}>
							{t("users.formDialog.cancel")}
						</DialogClose>
					)}
					{phase === "details" && hasRoleStep ? (
						<Button onClick={handleContinue}>
							{t("users.formDialog.continue")}
						</Button>
					) : (
						<Button onClick={handleSubmit} disabled={mutation.isPending}>
							{mutation.isPending
								? isEdit
									? t("users.formDialog.saving")
									: t("users.formDialog.creating")
								: isEdit
									? t("users.formDialog.saveChanges")
									: t("users.formDialog.createUser")}
						</Button>
					)}
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
