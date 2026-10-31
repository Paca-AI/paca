import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
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
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { disableMfa, regenerateRecoveryCodes } from "@/lib/mfa-api";

import { RecoveryCodes } from "./RecoveryCodes";

export type MfaCodeAction = "disable" | "regenerate";

/** Asks for a current code before a sensitive MFA change — turning MFA off
 * or replacing the recovery codes — so a hijacked session can't do either. */
export function MfaCodeDialog({
	action,
	onClose,
}: {
	action: MfaCodeAction | null;
	onClose: () => void;
}) {
	const { t } = useTranslation("profile");
	const queryClient = useQueryClient();
	const [code, setCode] = useState("");
	const [error, setError] = useState<string | null>(null);
	const [newCodes, setNewCodes] = useState<string[] | null>(null);

	useEffect(() => {
		if (action) {
			setCode("");
			setError(null);
			setNewCodes(null);
		}
	}, [action]);

	const mutation = useMutation({
		mutationFn: async () => {
			if (action === "disable") {
				await disableMfa(code);
				return null;
			}
			return regenerateRecoveryCodes(code);
		},
		onSuccess: (codes) => {
			void queryClient.invalidateQueries({ queryKey: ["auth", "mfa-status"] });
			if (codes) setNewCodes(codes);
			else onClose();
		},
		onError: (err) =>
			setError(
				getApiErrorCode(err) === ApiErrorCode.MfaInvalidCode
					? t("security.mfa.errors.invalidCode")
					: t("security.mfa.errors.generic"),
			),
	});

	const disabling = action === "disable";

	return (
		<Dialog
			open={action !== null}
			onOpenChange={(open) => {
				if (!open && !newCodes) onClose();
			}}
		>
			<DialogContent className="max-w-sm">
				<DialogHeader>
					<DialogTitle>
						{newCodes
							? t("security.mfa.regenerate.doneTitle")
							: disabling
								? t("security.mfa.disable.title")
								: t("security.mfa.regenerate.title")}
					</DialogTitle>
					<DialogDescription>
						{newCodes
							? t("security.mfa.setup.codesDescription")
							: disabling
								? t("security.mfa.disable.description")
								: t("security.mfa.regenerate.description")}
					</DialogDescription>
				</DialogHeader>

				{newCodes ? (
					<>
						<RecoveryCodes codes={newCodes} />
						<DialogFooter>
							<Button type="button" onClick={onClose}>
								{t("security.mfa.setup.saved")}
							</Button>
						</DialogFooter>
					</>
				) : (
					<form
						className="space-y-4"
						onSubmit={(e) => {
							e.preventDefault();
							if (code.trim()) mutation.mutate();
						}}
					>
						<div className="space-y-1.5">
							<Label htmlFor="mfa-action-code">
								{t("security.mfa.codeOrRecovery")}
							</Label>
							<Input
								id="mfa-action-code"
								autoComplete="one-time-code"
								value={code}
								onChange={(e) => setCode(e.target.value)}
							/>
						</div>
						{error ? <InlineNotice tone="error">{error}</InlineNotice> : null}
						<DialogFooter>
							<Button type="button" variant="outline" onClick={onClose}>
								{t("actions.cancel")}
							</Button>
							<Button
								type="submit"
								variant={disabling ? "destructive" : "default"}
								disabled={!code.trim() || mutation.isPending}
							>
								{mutation.isPending ? (
									<Loader2 className="size-4 animate-spin" />
								) : null}
								{disabling
									? t("security.mfa.disable.confirm")
									: t("security.mfa.regenerate.confirm")}
							</Button>
						</DialogFooter>
					</form>
				)}
			</DialogContent>
		</Dialog>
	);
}
