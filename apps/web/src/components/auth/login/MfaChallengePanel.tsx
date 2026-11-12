import { browserSupportsWebAuthn } from "@simplewebauthn/browser";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { AlertCircle, KeyRound, ShieldCheck } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Spinner } from "@/components/ui/spinner";
import {
	ApiErrorCode,
	getApiErrorCode,
	tooManyAttemptsMessage,
} from "@/lib/api-error";
import { verifyMfaLogin, verifyPasskeyLogin } from "@/lib/auth-api";
import { clearPendingMfa, type PendingMfa } from "@/lib/mfa-challenge";
import { cn } from "@/lib/utils";

/** Second step of signing in: the password was accepted, now a passkey, or
 * the code from the authenticator app (or a recovery code). */
export function MfaChallengePanel({ pending }: { pending: PendingMfa }) {
	const { t, i18n } = useTranslation("auth");
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const [code, setCode] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);

	// A server that predates passkeys sends no list: it only ever meant a code.
	const methods = pending.methods?.length ? pending.methods : ["totp"];
	const hasCode = methods.includes("totp");
	const hasPasskey = methods.includes("passkey") && browserSupportsWebAuthn();

	async function enter() {
		clearPendingMfa();
		await queryClient.invalidateQueries({ queryKey: ["auth"] });
		if (pending.redirectTo) {
			window.location.assign(pending.redirectTo);
			return;
		}
		await navigate({ to: "/home" });
	}

	async function submit() {
		if (!code.trim() || busy) return;
		setBusy(true);
		setError(null);
		try {
			await verifyMfaLogin(pending.challenge, code.trim());
			await enter();
		} catch (err: unknown) {
			await fail(err, "code");
		}
	}

	async function signInWithPasskey() {
		if (busy) return;
		setBusy(true);
		setError(null);
		try {
			await verifyPasskeyLogin(pending.challenge);
			await enter();
		} catch (err: unknown) {
			await fail(err, "passkey");
		}
	}

	async function fail(err: unknown, via: "code" | "passkey") {
		const errCode = getApiErrorCode(err);
		if (errCode === ApiErrorCode.TokenInvalid) {
			// The challenge expired (or was never valid): start over.
			clearPendingMfa();
			await navigate({
				to: "/",
				search: pending.redirectTo ? { redirect: pending.redirectTo } : {},
			});
			return;
		}
		let message: string;
		if (err instanceof Error && err.name === "NotAllowedError") {
			// The browser rejects this way when the prompt is dismissed or
			// times out: not a failure worth alarming over.
			message = t("login.errors.passkeyCancelled");
		} else if (errCode === ApiErrorCode.MfaInvalidCode) {
			message =
				via === "passkey"
					? t("login.errors.passkeyFailed")
					: t("login.errors.mfaInvalidCode");
		} else if (errCode === ApiErrorCode.IpNotAllowed) {
			message = t("login.errors.ipNotAllowed");
		} else if (errCode === ApiErrorCode.TooManyRequests) {
			message = tooManyAttemptsMessage(err, t, i18n.language);
		} else {
			message = t("login.errors.genericError");
		}
		setError(message);
		setBusy(false);
	}

	async function back() {
		clearPendingMfa();
		await navigate({
			to: "/",
			search: pending.redirectTo ? { redirect: pending.redirectTo } : {},
		});
	}

	const errorNotice = error ? (
		<div
			role="alert"
			className="flex items-start gap-2.5 rounded-lg border border-destructive/30 bg-destructive/10 px-3.5 py-3 text-sm text-destructive"
		>
			<AlertCircle className="mt-px size-4 shrink-0" />
			<span>{error}</span>
		</div>
	) : null;

	return (
		<div className="relative flex flex-col justify-center px-8 py-10 sm:px-10">
			<div className="mb-5 flex size-11 items-center justify-center rounded-xl bg-primary/10 ring-1 ring-primary/20">
				<ShieldCheck className="size-5 text-primary" />
			</div>
			<h1 className="display-title mb-1 text-2xl font-bold text-(--sea-ink) sm:text-3xl">
				{hasCode && hasPasskey
					? t("login.mfa.titleBoth")
					: hasCode
						? t("login.mfa.title")
						: t("login.mfa.titlePasskey")}
			</h1>
			<p className="mb-8 text-sm text-(--sea-ink-soft)">
				{hasCode && hasPasskey
					? t("login.mfa.subtitleBoth")
					: hasCode
						? t("login.mfa.subtitle")
						: t("login.mfa.subtitlePasskey")}
			</p>

			<div className="space-y-5">
				{hasPasskey ? (
					<button
						type="button"
						disabled={busy}
						onClick={() => void signInWithPasskey()}
						className={cn(
							buttonVariants({
								size: "lg",
								variant: hasCode ? "outline" : "default",
							}),
							"h-11 w-full font-semibold tracking-wide",
							!hasCode &&
								"bg-primary text-primary-foreground hover:bg-primary/90",
						)}
					>
						{busy ? (
							<Spinner aria-hidden="true" />
						) : (
							<KeyRound className="size-4" />
						)}
						{t("login.mfa.usePasskey")}
					</button>
				) : null}

				{!hasCode && !hasPasskey ? (
					<p className="text-sm text-(--sea-ink-soft)">
						{t("login.mfa.passkeyUnsupported")}
					</p>
				) : null}

				{hasCode && hasPasskey ? (
					<div className="flex items-center gap-3 text-xs text-(--sea-ink-soft) uppercase">
						<span className="h-px flex-1 bg-current opacity-20" />
						{t("login.mfa.or")}
						<span className="h-px flex-1 bg-current opacity-20" />
					</div>
				) : null}

				{hasCode ? (
					<form
						className="space-y-5"
						onSubmit={(e) => {
							e.preventDefault();
							void submit();
						}}
					>
						<div className="space-y-1.5">
							<Label
								htmlFor="mfa-code"
								className="text-xs font-semibold tracking-wide text-(--sea-ink) uppercase"
							>
								{t("login.mfa.codeLabel")}
							</Label>
							<Input
								id="mfa-code"
								name="code"
								inputMode="numeric"
								autoComplete="one-time-code"
								autoFocus={!hasPasskey}
								placeholder={t("login.mfa.codePlaceholder")}
								value={code}
								onChange={(e) => setCode(e.target.value)}
								className="h-10 font-mono tracking-widest"
							/>
							<p className="text-xs text-(--sea-ink-soft)">
								{t("login.mfa.hint")}
							</p>
						</div>

						{errorNotice}

						<button
							type="submit"
							disabled={busy || !code.trim()}
							className={cn(
								buttonVariants({ size: "lg" }),
								"h-11 w-full font-semibold tracking-wide bg-primary text-primary-foreground hover:bg-primary/90",
							)}
						>
							{busy ? <Spinner aria-hidden="true" /> : null}
							{busy ? t("login.mfa.verifying") : t("login.mfa.verify")}
						</button>
					</form>
				) : (
					errorNotice
				)}

				<button
					type="button"
					onClick={() => void back()}
					className="w-full text-center text-sm text-(--sea-ink-soft) hover:text-(--sea-ink)"
				>
					{t("login.mfa.back")}
				</button>
			</div>
		</div>
	);
}
