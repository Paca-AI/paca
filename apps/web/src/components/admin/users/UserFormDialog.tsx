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
import { useAssignUserRole } from "@/components/admin/users/use-assign-user-role";
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
	createUser,
	type GlobalRole,
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

/** An account the wizard has created, with what the last step shows about it. */
interface CreatedAccount {
	user: User;
	/** Generated for the account. Shown once, on the last step. */
	password: string;
	/** Name of the role it holds now: the default role until step 2 changes it. */
	role: string;
}

/** Where the create wizard is. Editing stays on "details". */
type Step =
	| { phase: "details" }
	| { phase: "role"; account: CreatedAccount }
	| { phase: "password"; account: CreatedAccount };

interface ProfileProblem {
	field: "username" | "fullName" | "email";
	message: string;
}

interface UserFormDialogProps {
	user?: User;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

/**
 * Creates a user or edits their profile (name and email).
 *
 * Creating is a short wizard: 1 Details → 2 Role → 3 Password. The account is
 * created at the end of step 1 — with the default role, which the server
 * assigns — and step 2 is a separate request that changes the role (its own
 * privilege, `global_roles.assign`, so the step exists only for someone who
 * has it; without it the wizard is Details → Password). The one-time password
 * is the very last thing shown, and since the account already exists by then
 * the role step cannot be closed without getting there. Editing never touches
 * the role: that is UserRoleDialog, opened from the users table.
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

	const [step, setStep] = useState<Step>({ phase: "details" });
	const [username, setUsername] = useState(user?.username ?? "");
	const [fullName, setFullName] = useState(user?.full_name ?? "");
	const [email, setEmail] = useState(user?.email ?? "");
	const [selectedRole, setSelectedRole] = useState<GlobalRole | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [usernameError, setUsernameError] = useState<string | null>(null);
	const [emailError, setEmailError] = useState<string | null>(null);
	const [showPassword, setShowPassword] = useState(false);
	const [copied, setCopied] = useState(false);

	const account = step.phase === "details" ? null : step.account;
	const stepNumber =
		step.phase === "details" ? 1 : step.phase === "role" ? 2 : totalSteps;

	// Step 2's request. It needs the account, so it targets no one until then.
	const {
		assign,
		isPending: assigning,
		error: assignError,
		clearError: clearAssignError,
	} = useAssignUserRole({
		userId: account?.user.id ?? "",
		onAssigned: (role) =>
			setStep((current) =>
				current.phase === "role"
					? {
							phase: "password",
							account: { ...current.account, role: role.name },
						}
					: current,
			),
	});

	const reset = () => {
		setStep({ phase: "details" });
		setUsername(user?.username ?? "");
		setFullName(user?.full_name ?? "");
		setEmail(user?.email ?? "");
		setSelectedRole(null);
		setError(null);
		setUsernameError(null);
		setEmailError(null);
		clearAssignError();
		setShowPassword(false);
		setCopied(false);
	};

	const mutation = useMutation({
		mutationFn: async (): Promise<{ user: User; password?: string }> => {
			if (isEdit && user) {
				const updated = await updateUser(user.id, {
					full_name: fullName.trim(),
					email: email.trim() || undefined,
				});
				return { user: updated };
			}

			const password = generatePassword();
			const created = await createUser({
				username: username.trim(),
				password,
				full_name: fullName.trim(),
				email: email.trim() || undefined,
			});
			return { user: created, password };
		},
		onSuccess: ({ user: saved, password }) => {
			void queryClient.invalidateQueries({
				queryKey: usersQueryOptions().queryKey.slice(0, 2),
			});
			if (password === undefined) {
				onOpenChange(false);
				reset();
				return;
			}
			// The account exists and holds the default role. Choosing another is a
			// request of its own, so it is the next step.
			const created = { user: saved, password, role: saved.role };
			setStep({ phase: hasRoleStep ? "role" : "password", account: created });
		},
		onError: (err: unknown) => {
			setError(null);
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
			const messages: Partial<Record<string, string>> = {
				[ApiErrorCode.UserNotFound]: t("users.formDialog.errors.userNotFound"),
				[ApiErrorCode.Forbidden]: t("users.formDialog.errors.forbidden"),
				[ApiErrorCode.GlobalRoleNoDefault]: t(
					"users.formDialog.errors.noDefaultRole",
				),
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

	const handleOpenChange = (next: boolean) => {
		if (next) {
			onOpenChange(true);
			return;
		}
		// What a request brings back has to land somewhere, so no closing mid-way.
		if (mutation.isPending || assigning) return;
		// The account exists but its password hasn't been shown yet, and closing
		// would lose it for good: closing here keeps the role as it is and moves on.
		if (step.phase === "role") {
			setStep({ phase: "password", account: step.account });
			return;
		}
		reset();
		onOpenChange(false);
	};

	const handleCopy = () => {
		if (step.phase !== "password") return;
		void navigator.clipboard.writeText(step.account.password).then(() => {
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

	const handleSubmit = () => {
		const problem = validateProfile();
		if (problem) {
			showProblem(problem);
			return;
		}
		setError(null);
		mutation.mutate();
	};

	// Picking the role the account already holds changes nothing.
	const change =
		step.phase === "role" &&
		selectedRole &&
		selectedRole.name !== step.account.role
			? selectedRole
			: null;

	const handleRoleStepAction = () => {
		if (step.phase !== "role") return;
		if (change) assign(change);
		else setStep({ phase: "password", account: step.account });
	};

	const HeaderIcon =
		step.phase === "password"
			? KeyRound
			: step.phase === "role"
				? ShieldCheck
				: UserRound;

	return (
		<Dialog open={open} onOpenChange={handleOpenChange}>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<div className="flex items-center justify-between gap-3 pr-7">
						<div className="flex items-center gap-2.5">
							<div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
								<HeaderIcon className="size-4" />
							</div>
							<DialogTitle className="text-base">
								{step.phase === "password"
									? t("users.formDialog.createdTitle")
									: isEdit
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
						{step.phase === "password" ? (
							<>
								<strong className="text-foreground">
									{step.account.user.username}
								</strong>{" "}
								{t("users.formDialog.createdDescriptionSuffix")}
							</>
						) : step.phase === "role" ? (
							t("users.formDialog.roleStepDescription", {
								username: step.account.user.username,
								role: step.account.role,
							})
						) : isEdit ? (
							t("users.formDialog.editDescription")
						) : (
							t("users.formDialog.createDescription")
						)}
					</DialogDescription>
				</DialogHeader>

				{step.phase === "details" ? (
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
				) : step.phase === "role" ? (
					<div className="flex flex-col gap-3 py-1">
						<RolePicker
							label={t("users.formDialog.roleStep.title")}
							value={selectedRole?.id ?? null}
							onChange={(role) => {
								setSelectedRole(role);
								clearAssignError();
							}}
							currentRoleName={step.account.role}
							disabled={assigning}
						/>
						{change && isFullAccessRole(change) ? (
							<InlineNotice tone="warning">
								{t("users.roleDialog.fullAccessWarning")}
							</InlineNotice>
						) : null}
						{assignError ? (
							<InlineNotice tone="error">{assignError}</InlineNotice>
						) : null}
					</div>
				) : (
					<div className="flex flex-col gap-4 py-1">
						<div className="flex flex-col gap-3">
							<Label className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
								{t("users.formDialog.temporaryPasswordLabel")}
							</Label>
							<div className="flex items-center gap-2">
								<div className="relative flex-1">
									<Input
										readOnly
										type={showPassword ? "text" : "password"}
										value={step.account.password}
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

						<section className="flex flex-col gap-2 border-t pt-4">
							<Label className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
								{t("users.formDialog.roleStep.title")}
							</Label>
							<span className="inline-flex w-fit items-center rounded-full border px-2 py-0.5 font-mono text-xs font-medium leading-none text-foreground/80">
								{step.account.role}
							</span>
						</section>
					</div>
				)}

				<DialogFooter>
					{step.phase === "details" ? (
						<>
							<DialogClose render={<Button variant="outline" />}>
								{t("users.formDialog.cancel")}
							</DialogClose>
							<Button onClick={handleSubmit} disabled={mutation.isPending}>
								{mutation.isPending
									? isEdit
										? t("users.formDialog.saving")
										: t("users.formDialog.creating")
									: isEdit
										? t("users.formDialog.saveChanges")
										: t("users.formDialog.createUser")}
							</Button>
						</>
					) : step.phase === "role" ? (
						<Button onClick={handleRoleStepAction} disabled={assigning}>
							{assigning
								? t("users.roleDialog.assigning")
								: change
									? t("users.roleDialog.assign")
									: t("users.formDialog.continue")}
						</Button>
					) : (
						<Button onClick={() => handleOpenChange(false)}>
							{t("users.formDialog.done")}
						</Button>
					)}
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
