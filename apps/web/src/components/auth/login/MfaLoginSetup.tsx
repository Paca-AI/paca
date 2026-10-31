import { useMutation } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { QrCode } from "@/components/profile/mfa/QrCode";
import { RecoveryCodes } from "@/components/profile/mfa/RecoveryCodes";
import { InlineNotice } from "@/components/shared/inline-notice";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	ApiErrorCode,
	getApiErrorCode,
	tooManyAttemptsMessage,
} from "@/lib/api-error";
import {
	beginLoginMfaSetup,
	confirmLoginMfaSetup,
	type MfaEnrollment,
} from "@/lib/mfa-api";

/** Two-step verification setup shown on the sign-in screen, for an account
 * that policy requires to use it. The password already checked out, so it is
 * what authorizes the setup; afterwards the person signs in with a code. */
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
	const [enrollment, setEnrollment] = useState<MfaEnrollment | null>(null);
	const [code, setCode] = useState("");
	const [codes, setCodes] = useState<string[] | null>(null);
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
		onSuccess: setCodes,
		onError: (err) =>
			setError(
				getApiErrorCode(err) === ApiErrorCode.MfaInvalidCode
					? t("login.errors.mfaInvalidCode")
					: getApiErrorCode(err) === ApiErrorCode.TooManyRequests
						? tooManyAttemptsMessage(err, t, i18n.language)
						: t("login.errors.genericError"),
			),
	});

	// biome-ignore lint/correctness/useExhaustiveDependencies: begin once on mount
	useEffect(() => {
		begin.mutate();
	}, []);

	return (
		<div className="space-y-4">
			<div>
				<h2 className="text-lg font-semibold text-(--sea-ink)">
					{codes ? t("login.mfaSetup.codesTitle") : t("login.mfaSetup.title")}
				</h2>
				<p className="mt-1 text-sm text-(--sea-ink-soft)">
					{codes ? t("login.mfaSetup.codesDesc") : t("login.mfaSetup.desc")}
				</p>
			</div>

			{codes ? (
				<>
					<RecoveryCodes codes={codes} />
					<Button className="w-full" onClick={onDone}>
						{t("login.mfaSetup.continue")}
					</Button>
				</>
			) : enrollment ? (
				<form
					className="space-y-4"
					onSubmit={(e) => {
						e.preventDefault();
						if (code.trim()) confirm.mutate();
					}}
				>
					<div className="flex flex-col items-center gap-3">
						<QrCode
							value={enrollment.uri}
							label={tProfile("security.mfa.setup.qrLabel")}
						/>
						<p className="text-center font-mono text-xs break-all select-all text-(--sea-ink-soft)">
							{enrollment.secret}
						</p>
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
							onChange={(e) => setCode(e.target.value)}
						/>
					</div>
					{error ? <InlineNotice tone="error">{error}</InlineNotice> : null}
					<div className="flex gap-2">
						<Button type="button" variant="outline" onClick={onCancel}>
							{tProfile("actions.cancel")}
						</Button>
						<Button
							type="submit"
							className="flex-1"
							disabled={!code.trim() || confirm.isPending}
						>
							{confirm.isPending ? (
								<Loader2 className="size-4 animate-spin" />
							) : null}
							{tProfile("security.mfa.setup.verify")}
						</Button>
					</div>
				</form>
			) : (
				<div className="space-y-3">
					{error ? (
						<>
							<InlineNotice tone="error">{error}</InlineNotice>
							<Button variant="outline" onClick={onCancel}>
								{tProfile("actions.cancel")}
							</Button>
						</>
					) : (
						<div className="flex min-h-40 items-center justify-center">
							<Loader2 className="size-5 animate-spin text-(--sea-ink-soft)" />
						</div>
					)}
				</div>
			)}
		</div>
	);
}
