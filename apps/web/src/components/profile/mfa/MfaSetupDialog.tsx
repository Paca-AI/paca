import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { InlineNotice } from "@/components/shared/inline-notice";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	ApiErrorCode,
	getApiErrorCode,
	tooManyAttemptsMessage,
} from "@/lib/api-error";
import { copyToClipboard } from "@/lib/clipboard";
import {
	beginMfaEnrollment,
	confirmMfaEnrollment,
	type MfaEnrollment,
} from "@/lib/mfa-api";

import { QrCode } from "./QrCode";
import { RecoveryCodes } from "./RecoveryCodes";

/** Three steps: scan the QR code, prove the app works with a code, then keep
 * the one-time recovery codes. The account is only protected after step 2. */
export function MfaSetupDialog({
	open,
	onOpenChange,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const { t } = useTranslation("profile");
	const { t: tAuth, i18n } = useTranslation("auth");
	const queryClient = useQueryClient();
	const [enrollment, setEnrollment] = useState<MfaEnrollment | null>(null);
	const [code, setCode] = useState("");
	const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [copied, setCopied] = useState(false);

	const begin = useMutation({
		mutationFn: beginMfaEnrollment,
		onSuccess: setEnrollment,
		onError: (err) =>
			setError(
				getApiErrorCode(err) === ApiErrorCode.MfaUnavailable
					? t("security.mfa.errors.unavailable")
					: t("security.mfa.errors.generic"),
			),
	});

	const confirm = useMutation({
		mutationFn: () => confirmMfaEnrollment(code),
		onSuccess: (codes) => {
			setRecoveryCodes(codes);
			setError(null);
			void queryClient.invalidateQueries({ queryKey: ["auth", "mfa-status"] });
		},
		onError: (err) =>
			setError(
				getApiErrorCode(err) === ApiErrorCode.MfaInvalidCode
					? t("security.mfa.errors.invalidCode")
					: getApiErrorCode(err) === ApiErrorCode.TooManyRequests
						? tooManyAttemptsMessage(err, tAuth, i18n.language)
						: t("security.mfa.errors.generic"),
			),
	});

	// A fresh secret each time the dialog opens; nothing leaks between runs.
	// biome-ignore lint/correctness/useExhaustiveDependencies: begin.mutate is stable
	useEffect(() => {
		if (!open) return;
		setEnrollment(null);
		setCode("");
		setRecoveryCodes(null);
		setError(null);
		begin.mutate();
	}, [open]);

	async function copySecret() {
		if (enrollment && (await copyToClipboard(enrollment.secret))) {
			setCopied(true);
			setTimeout(() => setCopied(false), 2000);
		}
	}

	const done = recoveryCodes !== null;

	return (
		<Dialog
			open={open}
			// Once the codes are shown they cannot be fetched again, so closing is
			// only allowed through the explicit "I've saved them" button.
			onOpenChange={(next) => {
				if (!done) onOpenChange(next);
			}}
		>
			<DialogContent className="max-w-md">
				<DialogHeader>
					<DialogTitle>
						{done
							? t("security.mfa.setup.codesTitle")
							: t("security.mfa.setup.title")}
					</DialogTitle>
					<DialogDescription>
						{done
							? t("security.mfa.setup.codesDescription")
							: t("security.mfa.setup.description")}
					</DialogDescription>
				</DialogHeader>

				{done ? (
					<div className="space-y-3">
						<RecoveryCodes codes={recoveryCodes} />
						<InlineNotice tone="warning">
							{t("security.mfa.setup.codesWarning")}
						</InlineNotice>
					</div>
				) : enrollment ? (
					<form
						className="space-y-4"
						onSubmit={(e) => {
							e.preventDefault();
							if (code.trim()) confirm.mutate();
						}}
					>
						<div className="flex flex-col items-center gap-3 sm:flex-row sm:items-start">
							<QrCode
								value={enrollment.uri}
								label={t("security.mfa.setup.qrLabel")}
							/>
							<div className="min-w-0 space-y-2 text-sm">
								<p className="text-muted-foreground">
									{t("security.mfa.setup.scan")}
								</p>
								<p className="text-xs text-muted-foreground">
									{t("security.mfa.setup.orEnterKey")}
								</p>
								<button
									type="button"
									onClick={copySecret}
									className="flex w-full items-center gap-2 rounded-md border border-border/60 bg-muted/40 px-2 py-1.5 text-left font-mono text-xs break-all hover:bg-muted"
									aria-label={t("security.mfa.setup.copyKey")}
								>
									<span className="min-w-0 flex-1 select-all">
										{enrollment.secret}
									</span>
									{copied ? (
										<Check className="size-3.5 shrink-0" />
									) : (
										<Copy className="size-3.5 shrink-0" />
									)}
								</button>
							</div>
						</div>

						<div className="space-y-1.5">
							<Label htmlFor="mfa-setup-code">
								{t("security.mfa.setup.codeLabel")}
							</Label>
							<Input
								id="mfa-setup-code"
								inputMode="numeric"
								autoComplete="one-time-code"
								placeholder="123456"
								value={code}
								onChange={(e) => setCode(e.target.value)}
							/>
						</div>

						{error ? <InlineNotice tone="error">{error}</InlineNotice> : null}

						<DialogFooter>
							<Button
								type="button"
								variant="outline"
								onClick={() => onOpenChange(false)}
							>
								{t("actions.cancel")}
							</Button>
							<Button
								type="submit"
								disabled={!code.trim() || confirm.isPending}
							>
								{confirm.isPending ? (
									<Loader2 className="size-4 animate-spin" />
								) : null}
								{t("security.mfa.setup.verify")}
							</Button>
						</DialogFooter>
					</form>
				) : (
					<div className="flex min-h-40 items-center justify-center">
						{error ? (
							<InlineNotice tone="error">{error}</InlineNotice>
						) : (
							<Loader2 className="size-5 animate-spin text-muted-foreground" />
						)}
					</div>
				)}

				{done ? (
					<DialogFooter>
						<Button type="button" onClick={() => onOpenChange(false)}>
							{t("security.mfa.setup.saved")}
						</Button>
					</DialogFooter>
				) : null}
			</DialogContent>
		</Dialog>
	);
}
