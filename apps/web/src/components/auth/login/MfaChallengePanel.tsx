import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { AlertCircle, Loader2, ShieldCheck } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	ApiErrorCode,
	getApiErrorCode,
	tooManyAttemptsMessage,
} from "@/lib/api-error";
import { verifyMfaLogin } from "@/lib/auth-api";
import { clearPendingMfa, type PendingMfa } from "@/lib/mfa-challenge";
import { cn } from "@/lib/utils";

/** Second step of signing in: the password was accepted, now the code from
 * the authenticator app (or a recovery code). */
export function MfaChallengePanel({ pending }: { pending: PendingMfa }) {
	const { t, i18n } = useTranslation("auth");
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const [code, setCode] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);

	async function submit() {
		if (!code.trim() || busy) return;
		setBusy(true);
		setError(null);
		try {
			await verifyMfaLogin(pending.challenge, code.trim());
			clearPendingMfa();
			await queryClient.invalidateQueries({ queryKey: ["auth"] });
			if (pending.redirectTo) {
				window.location.assign(pending.redirectTo);
				return;
			}
			await navigate({ to: "/home" });
		} catch (err: unknown) {
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
			setError(
				errCode === ApiErrorCode.MfaInvalidCode
					? t("login.errors.mfaInvalidCode")
					: errCode === ApiErrorCode.IpNotAllowed
						? t("login.errors.ipNotAllowed")
						: errCode === ApiErrorCode.TooManyRequests
							? tooManyAttemptsMessage(err, t, i18n.language)
							: t("login.errors.genericError"),
			);
			setBusy(false);
		}
	}

	async function back() {
		clearPendingMfa();
		await navigate({
			to: "/",
			search: pending.redirectTo ? { redirect: pending.redirectTo } : {},
		});
	}

	return (
		<div className="relative flex flex-col justify-center px-8 py-10 sm:px-10">
			<div className="mb-5 flex size-11 items-center justify-center rounded-xl bg-primary/10 ring-1 ring-primary/20">
				<ShieldCheck className="size-5 text-primary" />
			</div>
			<h1 className="display-title mb-1 text-2xl font-bold text-(--sea-ink) sm:text-3xl">
				{t("login.mfa.title")}
			</h1>
			<p className="mb-8 text-sm text-(--sea-ink-soft)">
				{t("login.mfa.subtitle")}
			</p>

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
						// biome-ignore lint/a11y/noAutofocus: the code is the only thing on this page
						autoFocus
						placeholder={t("login.mfa.codePlaceholder")}
						value={code}
						onChange={(e) => setCode(e.target.value)}
						className="h-10 font-mono tracking-widest"
					/>
					<p className="text-xs text-(--sea-ink-soft)">{t("login.mfa.hint")}</p>
				</div>

				{error ? (
					<div
						role="alert"
						className="flex items-start gap-2.5 rounded-lg border border-red-200 bg-red-50 px-3.5 py-3 text-sm text-red-700 dark:border-red-800/60 dark:bg-red-950/30 dark:text-red-400"
					>
						<AlertCircle className="mt-px size-4 shrink-0" />
						<span>{error}</span>
					</div>
				) : null}

				<button
					type="submit"
					disabled={busy || !code.trim()}
					className={cn(
						buttonVariants({ size: "lg" }),
						"h-11 w-full font-semibold tracking-wide bg-primary text-primary-foreground hover:bg-primary/90",
					)}
				>
					{busy ? <Loader2 className="size-4 animate-spin" /> : null}
					{busy ? t("login.mfa.verifying") : t("login.mfa.verify")}
				</button>

				<button
					type="button"
					onClick={() => void back()}
					className="w-full text-center text-sm text-(--sea-ink-soft) hover:text-(--sea-ink)"
				>
					{t("login.mfa.back")}
				</button>
			</form>
		</div>
	);
}
