import { browserSupportsWebAuthn } from "@simplewebauthn/browser";
import { useMutation } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import {
	AlertCircle,
	Check,
	Copy,
	KeyRound,
	ShieldCheck,
	Smartphone,
} from "lucide-react";
import { type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import { QrCode } from "@/components/profile/mfa/QrCode";
import { RecoveryCodes } from "@/components/profile/mfa/RecoveryCodes";
import { Button, buttonVariants } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Spinner } from "@/components/ui/spinner";
import {
	ApiErrorCode,
	getApiErrorCode,
	tooManyAttemptsMessage,
} from "@/lib/api-error";
import { copyToClipboard } from "@/lib/clipboard";
import {
	beginLoginMfaSetup,
	confirmLoginMfaSetup,
	type MfaEnrollment,
	registerLoginPasskey,
} from "@/lib/mfa-api";
import { cn } from "@/lib/utils";

type Method = "passkey" | "app";

/** Where the person is. `passkey` covers the browser prompt and its failure;
 * `codes` is the authenticator-app path's last step. */
type Step = "choose" | "passkey" | "passkeyDone" | "app" | "codes";

/** Two-step verification setup shown on the sign-in screen, for an account
 * that policy requires to use it: a passkey or an authenticator app, the
 * person's choice. The password already checked out, so it is what authorizes
 * the setup; afterwards the person signs in with the factor they set up. */
export function MfaLoginSetup({
	username,
	password,
	onDone,
	onCancel,
}: {
	username: string;
	password: string;
	onDone: () => void;
	onCancel: () => void;
}) {
	const { t, i18n } = useTranslation("auth");
	const { t: tProfile } = useTranslation("profile");
	const passkeysSupported = browserSupportsWebAuthn();
	const [method, setMethod] = useState<Method>(
		passkeysSupported ? "passkey" : "app",
	);
	const [step, setStep] = useState<Step>("choose");
	const [enrollment, setEnrollment] = useState<MfaEnrollment | null>(null);
	const [code, setCode] = useState("");
	const [codes, setCodes] = useState<string[] | null>(null);
	const [saved, setSaved] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const begin = useMutation({
		mutationFn: () => beginLoginMfaSetup(username, password),
		onSuccess: setEnrollment,
		onError: (err) =>
			setError(
				getApiErrorCode(err) === ApiErrorCode.MfaUnavailable
					? tProfile("security.mfa.errors.unavailable")
					: t("login.errors.genericError"),
			),
	});
	const confirm = useMutation({
		mutationFn: () => confirmLoginMfaSetup(username, password, code),
		onSuccess: (recovery) => {
			setCodes(recovery);
			setStep("codes");
		},
		onError: (err) =>
			setError(
				getApiErrorCode(err) === ApiErrorCode.MfaInvalidCode
					? t("login.errors.mfaInvalidCode")
					: getApiErrorCode(err) === ApiErrorCode.TooManyRequests
						? tooManyAttemptsMessage(err, t, i18n.language)
						: t("login.errors.genericError"),
			),
	});
	const passkey = useMutation({
		mutationFn: () =>
			registerLoginPasskey(
				username,
				password,
				tProfile("security.passkeys.defaultName"),
			),
		onSuccess: () => setStep("passkeyDone"),
		onError: (err) => {
			const errCode = getApiErrorCode(err);
			setError(
				// Dismissing the browser's prompt rejects with NotAllowedError.
				err instanceof Error && err.name === "NotAllowedError"
					? t("login.errors.passkeyCancelled")
					: err instanceof Error && err.name === "InvalidStateError"
						? tProfile("security.passkeys.errors.alreadyRegistered")
						: errCode === ApiErrorCode.MfaUnavailable
							? t("login.mfaSetup.passkeyUnavailable")
							: errCode === ApiErrorCode.TooManyRequests
								? tooManyAttemptsMessage(err, t, i18n.language)
								: t("login.errors.genericError"),
			);
		},
	});

	function start() {
		setError(null);
		if (method === "passkey") {
			setStep("passkey");
			passkey.mutate();
		} else {
			setStep("app");
			begin.mutate();
		}
	}

	function backToChoice() {
		setError(null);
		setCode("");
		setEnrollment(null);
		passkey.reset();
		begin.reset();
		setStep("choose");
	}

	// The app path has a scan step and a save step more than the passkey path.
	const total = method === "app" ? 3 : 2;
	const current = step === "choose" ? 1 : step === "codes" ? 3 : 2;

	return (
		<div className="space-y-6">
			<header className="space-y-3">
				<div className="flex items-center gap-3">
					<div className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-primary/10 ring-1 ring-primary/20">
						<ShieldCheck className="size-5 text-primary" />
					</div>
					<div className="min-w-0 flex-1 space-y-1.5">
						<p className="text-xs text-(--sea-ink-soft)" aria-live="polite">
							{t("login.mfaSetup.progress", { current, total })}
						</p>
						<div className="flex gap-1" aria-hidden="true">
							{Array.from({ length: total }, (_, i) => (
								<span
									// biome-ignore lint/suspicious/noArrayIndexKey: fixed-length decorative segments
									key={i}
									className={cn(
										"h-1 flex-1 rounded-full transition-colors",
										i < current ? "bg-primary" : "bg-border",
									)}
								/>
							))}
						</div>
					</div>
				</div>
				<div>
					<h2 className="display-title text-xl font-bold text-(--sea-ink)">
						{stepTitle(step, t)}
					</h2>
					<p className="mt-1 text-sm text-(--sea-ink-soft)">
						{stepIntro(step, t, username)}
					</p>
				</div>
			</header>

			{step === "choose" ? (
				<form
					className="space-y-4"
					onSubmit={(e) => {
						e.preventDefault();
						start();
					}}
				>
					<fieldset className="space-y-2.5">
						<legend className="sr-only">{t("login.mfaSetup.title")}</legend>
						{passkeysSupported ? (
							<MethodCard
								value="passkey"
								selected={method === "passkey"}
								onSelect={setMethod}
								icon={<KeyRound className="size-5" />}
								title={t("login.mfaSetup.passkey.title")}
								hint={t("login.mfaSetup.passkey.desc")}
								badge={t("login.mfaSetup.recommended")}
							/>
						) : null}
						<MethodCard
							value="app"
							selected={method === "app"}
							onSelect={setMethod}
							icon={<Smartphone className="size-5" />}
							title={t("login.mfaSetup.app.title")}
							hint={t("login.mfaSetup.app.desc")}
						/>
					</fieldset>
					<Button type="submit" className="h-11 w-full font-semibold">
						{t("login.mfaSetup.continue")}
					</Button>
					<BackLink onClick={onCancel}>
						{t("login.mfaSetup.backToSignIn")}
					</BackLink>
				</form>
			) : null}

			{step === "passkey" ? (
				<div className="space-y-4">
					{passkey.isPending ? (
						<div
							className="flex flex-col items-center gap-3 rounded-lg border border-dashed border-border px-4 py-8 text-center"
							aria-live="polite"
						>
							<Spinner aria-hidden="true" className="size-6 text-primary" />
							<p className="text-sm text-(--sea-ink-soft)">
								{t("login.mfaSetup.passkeyWaiting")}
							</p>
						</div>
					) : (
						<>
							{error ? <ErrorNotice>{error}</ErrorNotice> : null}
							<Button
								className="h-11 w-full font-semibold"
								onClick={() => {
									setError(null);
									passkey.mutate();
								}}
							>
								{t("login.mfaSetup.tryAgain")}
							</Button>
							<BackLink onClick={backToChoice}>
								{t("login.mfaSetup.chooseAnother")}
							</BackLink>
						</>
					)}
				</div>
			) : null}

			{step === "passkeyDone" ? (
				<div className="space-y-4">
					<div className="flex items-center gap-3 rounded-lg border border-success/30 bg-success/10 px-3.5 py-3 text-sm text-success">
						<Check className="size-4 shrink-0" />
						<span>{t("login.mfaSetup.passkeyDone.notice")}</span>
					</div>
					<Button className="h-11 w-full font-semibold" onClick={onDone}>
						{t("login.mfaSetup.finish")}
					</Button>
				</div>
			) : null}

			{step === "app" ? (
				<AppStep
					enrollment={enrollment}
					loadError={begin.isError ? error : null}
					code={code}
					onCode={setCode}
					error={confirm.isError ? error : null}
					pending={confirm.isPending}
					onSubmit={() => {
						setError(null);
						confirm.mutate();
					}}
					onBack={backToChoice}
				/>
			) : null}

			{step === "codes" && codes ? (
				<div className="space-y-4">
					<RecoveryCodes codes={codes} />
					<label className="flex cursor-pointer items-start gap-2.5 text-sm text-(--sea-ink)">
						<Checkbox
							checked={saved}
							onCheckedChange={(value) => setSaved(value)}
						/>
						<span>{t("login.mfaSetup.codes.confirm")}</span>
					</label>
					<Button
						className="h-11 w-full font-semibold"
						disabled={!saved}
						onClick={onDone}
					>
						{t("login.mfaSetup.finish")}
					</Button>
				</div>
			) : null}
		</div>
	);
}

function stepTitle(step: Step, t: TFunction<"auth">): string {
	switch (step) {
		case "passkey":
			return t("login.mfaSetup.passkeyStep.title");
		case "passkeyDone":
			return t("login.mfaSetup.passkeyDone.title");
		case "app":
			return t("login.mfaSetup.appStep.title");
		case "codes":
			return t("login.mfaSetup.codes.title");
		default:
			return t("login.mfaSetup.title");
	}
}

function stepIntro(step: Step, t: TFunction<"auth">, username: string): string {
	switch (step) {
		case "passkey":
			return t("login.mfaSetup.passkeyStep.desc");
		case "passkeyDone":
			return t("login.mfaSetup.passkeyDone.desc");
		case "app":
			return t("login.mfaSetup.appStep.desc");
		case "codes":
			return t("login.mfaSetup.codes.desc");
		default:
			return t("login.mfaSetup.subtitle", { username });
	}
}

function MethodCard({
	value,
	selected,
	onSelect,
	icon,
	title,
	hint,
	badge,
}: {
	value: Method;
	selected: boolean;
	onSelect: (value: Method) => void;
	icon: ReactNode;
	title: string;
	hint: string;
	badge?: string;
}) {
	return (
		<label
			className={cn(
				"flex cursor-pointer items-start gap-3 rounded-lg border p-3.5 transition-colors",
				"focus-within:ring-2 focus-within:ring-ring/60",
				selected
					? "border-primary bg-primary/5"
					: "border-border hover:bg-muted/50",
			)}
		>
			<input
				type="radio"
				name="mfa-setup-method"
				value={value}
				checked={selected}
				onChange={() => onSelect(value)}
				className="sr-only"
			/>
			<span
				className={cn(
					"mt-0.5",
					selected ? "text-primary" : "text-(--sea-ink-soft)",
				)}
			>
				{icon}
			</span>
			<span className="min-w-0 flex-1">
				<span className="flex flex-wrap items-center gap-2">
					<span className="text-sm font-semibold text-(--sea-ink)">
						{title}
					</span>
					{badge ? (
						<span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary">
							{badge}
						</span>
					) : null}
				</span>
				<span className="mt-0.5 block text-sm text-(--sea-ink-soft)">
					{hint}
				</span>
			</span>
			<span
				aria-hidden="true"
				className={cn(
					"mt-1 flex size-4 shrink-0 items-center justify-center rounded-full border",
					selected
						? "border-primary bg-primary text-primary-foreground"
						: "border-border",
				)}
			>
				{selected ? <Check className="size-3" /> : null}
			</span>
		</label>
	);
}

function AppStep({
	enrollment,
	loadError,
	code,
	onCode,
	error,
	pending,
	onSubmit,
	onBack,
}: {
	enrollment: MfaEnrollment | null;
	loadError: string | null;
	code: string;
	onCode: (code: string) => void;
	error: string | null;
	pending: boolean;
	onSubmit: () => void;
	onBack: () => void;
}) {
	const { t } = useTranslation("auth");
	const { t: tProfile } = useTranslation("profile");
	const [showKey, setShowKey] = useState(false);
	const [copied, setCopied] = useState(false);

	if (!enrollment) {
		return loadError ? (
			<div className="space-y-4">
				<ErrorNotice>{loadError}</ErrorNotice>
				<BackLink onClick={onBack}>
					{t("login.mfaSetup.chooseAnother")}
				</BackLink>
			</div>
		) : (
			<div className="flex min-h-40 items-center justify-center">
				<Spinner aria-hidden="true" className="size-5 text-(--sea-ink-soft)" />
			</div>
		);
	}

	return (
		<form
			className="space-y-4"
			onSubmit={(e) => {
				e.preventDefault();
				if (code.trim()) onSubmit();
			}}
		>
			<div className="flex flex-col items-center gap-2">
				<QrCode
					value={enrollment.uri}
					label={tProfile("security.mfa.setup.qrLabel")}
				/>
				{showKey ? (
					<div className="flex w-full items-center gap-2 rounded-md bg-muted/50 px-3 py-2">
						<p className="min-w-0 flex-1 font-mono text-xs break-all select-all text-(--sea-ink)">
							{enrollment.secret}
						</p>
						<button
							type="button"
							aria-label={tProfile("security.mfa.setup.copyKey")}
							className={cn(
								buttonVariants({ variant: "ghost", size: "icon" }),
								"size-7",
							)}
							onClick={async () => {
								if (await copyToClipboard(enrollment.secret)) {
									setCopied(true);
									setTimeout(() => setCopied(false), 2000);
								}
							}}
						>
							{copied ? (
								<Check className="size-4" />
							) : (
								<Copy className="size-4" />
							)}
						</button>
					</div>
				) : (
					<button
						type="button"
						onClick={() => setShowKey(true)}
						className="text-xs text-(--sea-ink-soft) underline-offset-2 hover:text-(--sea-ink) hover:underline"
					>
						{tProfile("security.mfa.setup.orEnterKey")}
					</button>
				)}
			</div>

			<div className="space-y-1.5">
				<Label htmlFor="login-mfa-setup-code">
					{tProfile("security.mfa.setup.codeLabel")}
				</Label>
				<Input
					id="login-mfa-setup-code"
					inputMode="numeric"
					autoComplete="one-time-code"
					placeholder="123456"
					value={code}
					onChange={(e) => onCode(e.target.value)}
					className="h-10 font-mono tracking-widest"
				/>
			</div>

			{error ? <ErrorNotice>{error}</ErrorNotice> : null}

			<Button
				type="submit"
				className="h-11 w-full font-semibold"
				disabled={!code.trim() || pending}
			>
				{pending ? <Spinner aria-hidden="true" /> : null}
				{tProfile("security.mfa.setup.verify")}
			</Button>
			<BackLink onClick={onBack}>{t("login.mfaSetup.back")}</BackLink>
		</form>
	);
}

function ErrorNotice({ children }: { children: ReactNode }) {
	return (
		<div
			role="alert"
			className="flex items-start gap-2.5 rounded-lg border border-destructive/30 bg-destructive/10 px-3.5 py-3 text-sm text-destructive"
		>
			<AlertCircle className="mt-px size-4 shrink-0" />
			<span>{children}</span>
		</div>
	);
}

function BackLink({
	onClick,
	children,
}: {
	onClick: () => void;
	children: ReactNode;
}) {
	return (
		<button
			type="button"
			onClick={onClick}
			className="w-full text-center text-sm text-(--sea-ink-soft) hover:text-(--sea-ink)"
		>
			{children}
		</button>
	);
}
