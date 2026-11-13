import { browserSupportsWebAuthn } from "@simplewebauthn/browser";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { InlineNotice } from "@/components/shared/inline-notice";
import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
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
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import {
	deletePasskey,
	mfaStatusQueryOptions,
	type Passkey,
	passkeysQueryOptions,
	registerPasskey,
} from "@/lib/mfa-api";

/** Passkeys as a second sign-in factor, next to the authenticator app: add
 * one from this device or a security key, see when each was last used, and
 * remove it. Hidden when the server has no relying-party identity. */
export function PasskeysCard() {
	const { t } = useTranslation("profile");
	const { data: status } = useQuery(mfaStatusQueryOptions);
	const available = status?.passkeys_available === true;
	const {
		data: passkeys,
		isPending,
		isError,
	} = useQuery({
		...passkeysQueryOptions,
		enabled: available,
	});
	const [adding, setAdding] = useState(false);
	const [removing, setRemoving] = useState<Passkey | null>(null);

	if (!available) return null;

	const supported = browserSupportsWebAuthn();

	return (
		<Card>
			<CardHeader>
				<div className="flex flex-wrap items-start justify-between gap-3">
					<div>
						<CardTitle className="flex items-center gap-2">
							<KeyRound className="size-4 text-muted-foreground" />
							{t("security.passkeys.title")}
						</CardTitle>
						<CardDescription className="mt-1">
							{t("security.passkeys.description")}
						</CardDescription>
					</div>
					<Button
						size="sm"
						disabled={!supported}
						onClick={() => setAdding(true)}
					>
						{t("security.passkeys.add")}
					</Button>
				</div>
			</CardHeader>
			<CardContent className="space-y-3">
				{!supported ? (
					<InlineNotice tone="warning">
						{t("security.passkeys.unsupported")}
					</InlineNotice>
				) : null}
				{isPending ? (
					<Skeleton className="h-12 w-full" />
				) : isError ? (
					<InlineNotice tone="error">
						{t("security.passkeys.errors.load")}
					</InlineNotice>
				) : passkeys.length === 0 ? (
					<p className="text-sm text-muted-foreground">
						{t("security.passkeys.empty")}
					</p>
				) : (
					<ul className="divide-y rounded-md border">
						{passkeys.map((p) => (
							<li
								key={p.id}
								className="flex items-center justify-between gap-3 px-3 py-2.5"
							>
								<div className="min-w-0">
									<p className="truncate text-sm font-medium">{p.name}</p>
									<p className="text-xs text-muted-foreground">
										{t("security.passkeys.added", {
											date: new Date(p.created_at).toLocaleDateString(),
										})}
										{" · "}
										{p.last_used_at
											? t("security.passkeys.lastUsed", {
													date: new Date(p.last_used_at).toLocaleDateString(),
												})
											: t("security.passkeys.neverUsed")}
									</p>
								</div>
								<Button
									size="icon"
									variant="ghost"
									aria-label={t("security.passkeys.removeLabel", {
										name: p.name,
									})}
									onClick={() => setRemoving(p)}
								>
									<Trash2 className="size-4" />
								</Button>
							</li>
						))}
					</ul>
				)}
			</CardContent>

			<AddPasskeyDialog open={adding} onOpenChange={setAdding} />
			<RemovePasskeyDialog
				passkey={removing}
				onClose={() => setRemoving(null)}
			/>
		</Card>
	);
}

function AddPasskeyDialog({
	open,
	onOpenChange,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const { t } = useTranslation("profile");
	const queryClient = useQueryClient();
	const [name, setName] = useState("");
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: () =>
			registerPasskey(name.trim() || t("security.passkeys.defaultName")),
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: ["auth", "passkeys"] });
			onOpenChange(false);
		},
		onError: (err) => {
			const code = getApiErrorCode(err);
			setError(
				// Dismissing the browser's prompt rejects with NotAllowedError;
				// a credential this authenticator already holds, InvalidStateError.
				err instanceof Error && err.name === "NotAllowedError"
					? t("security.passkeys.errors.cancelled")
					: err instanceof Error && err.name === "InvalidStateError"
						? t("security.passkeys.errors.alreadyRegistered")
						: code === ApiErrorCode.PasskeyLimit
							? t("security.passkeys.errors.limit")
							: code === ApiErrorCode.PasskeyInvalid
								? t("security.passkeys.errors.invalid")
								: t("security.passkeys.errors.generic"),
			);
		},
	});

	// The dialog is opened by a button outside it, so Radix never reports the
	// opening: clear the last attempt here, each time it opens.
	// biome-ignore lint/correctness/useExhaustiveDependencies: reset only when it opens
	useEffect(() => {
		if (open) {
			setName("");
			setError(null);
			mutation.reset();
		}
	}, [open]);

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="max-w-sm">
				<DialogHeader>
					<DialogTitle>{t("security.passkeys.addTitle")}</DialogTitle>
					<DialogDescription>
						{t("security.passkeys.addDescription")}
					</DialogDescription>
				</DialogHeader>
				<form
					className="space-y-4"
					onSubmit={(e) => {
						e.preventDefault();
						setError(null);
						mutation.mutate();
					}}
				>
					<div className="space-y-1.5">
						<Label htmlFor="passkey-name">
							{t("security.passkeys.nameLabel")}
						</Label>
						<Input
							id="passkey-name"
							maxLength={64}
							placeholder={t("security.passkeys.namePlaceholder")}
							value={name}
							onChange={(e) => setName(e.target.value)}
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
						<Button type="submit" disabled={mutation.isPending}>
							{mutation.isPending ? <Spinner aria-hidden="true" /> : null}
							{t("security.passkeys.continue")}
						</Button>
					</DialogFooter>
				</form>
			</DialogContent>
		</Dialog>
	);
}

function RemovePasskeyDialog({
	passkey,
	onClose,
}: {
	passkey: Passkey | null;
	onClose: () => void;
}) {
	const { t } = useTranslation("profile");
	const queryClient = useQueryClient();
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: () => {
			if (!passkey) throw new Error("no passkey selected");
			return deletePasskey(passkey.id);
		},
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: ["auth", "passkeys"] });
			onClose();
		},
		onError: (err) => {
			const code = getApiErrorCode(err);
			setError(
				// Dismissing the browser's prompt rejects with NotAllowedError.
				err instanceof Error && err.name === "NotAllowedError"
					? t("security.passkeys.errors.cancelled")
					: code === ApiErrorCode.PasskeyInvalid
						? t("security.passkeys.errors.invalid")
						: t("security.passkeys.errors.generic"),
			);
		},
	});

	return (
		<Dialog
			open={passkey !== null}
			onOpenChange={(open) => {
				if (open) return;
				setError(null);
				mutation.reset();
				onClose();
			}}
		>
			<DialogContent className="max-w-sm">
				<DialogHeader>
					<DialogTitle>{t("security.passkeys.removeTitle")}</DialogTitle>
					<DialogDescription>
						{t("security.passkeys.removeDescription", {
							name: passkey?.name ?? "",
						})}
					</DialogDescription>
				</DialogHeader>
				{error ? <InlineNotice tone="error">{error}</InlineNotice> : null}
				<DialogFooter>
					<Button type="button" variant="outline" onClick={onClose}>
						{t("actions.cancel")}
					</Button>
					<Button
						type="button"
						variant="destructive"
						disabled={mutation.isPending}
						onClick={() => {
							setError(null);
							mutation.mutate();
						}}
					>
						{mutation.isPending ? <Spinner aria-hidden="true" /> : null}
						{t("security.passkeys.remove")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
