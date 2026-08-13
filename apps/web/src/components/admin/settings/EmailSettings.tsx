import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Mail, Send } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { getApiErrorMessage } from "@/lib/api-error";
import {
	type EmailSettings as EmailSettingsData,
	emailSettingsQueryOptions,
	sendTestEmail,
	updateEmailSettings,
} from "@/lib/email-settings-api";

export function EmailSettings() {
	const { t } = useTranslation("admin");
	const queryClient = useQueryClient();
	const { data: settings } = useQuery(emailSettingsQueryOptions);

	const [sendOnCreate, setSendOnCreate] = useState(
		settings?.send_user_created_email ?? true,
	);
	const [fromEmail, setFromEmail] = useState(settings?.from_email ?? "");
	const [fromName, setFromName] = useState(settings?.from_name ?? "");
	const [host, setHost] = useState(settings?.host ?? "");
	const [port, setPort] = useState<string>(
		settings?.port ? String(settings.port) : "",
	);
	const [username, setUsername] = useState(settings?.username ?? "");
	// Blank = keep the stored password (the API never returns it).
	const [password, setPassword] = useState("");
	const [useSSL, setUseSSL] = useState(settings?.use_ssl ?? false);
	const [useTLS, setUseTLS] = useState(settings?.use_tls ?? false);
	const [skipVerify, setSkipVerify] = useState(settings?.skip_verify ?? false);

	const [error, setError] = useState<string | null>(null);
	const [saved, setSaved] = useState(false);

	const [testTo, setTestTo] = useState("");
	const [testMsg, setTestMsg] = useState<{
		ok: boolean;
		text: string;
	} | null>(null);

	const patchCache = (updated: EmailSettingsData) => {
		queryClient.setQueryData(emailSettingsQueryOptions.queryKey, updated);
	};

	const saveMutation = useMutation({
		mutationFn: () =>
			updateEmailSettings({
				from_email: fromEmail.trim(),
				from_name: fromName.trim(),
				host: host.trim(),
				port: Number(port) || 0,
				username: username.trim(),
				// Only send the password field when the admin typed a new one;
				// blank keeps the stored value.
				password: password === "" ? undefined : password,
				use_ssl: useSSL,
				use_tls: useTLS,
				skip_verify: skipVerify,
				send_user_created_email: sendOnCreate,
			}),
		onSuccess: (updated) => {
			patchCache(updated);
			setPassword("");
			setError(null);
			setSaved(true);
			setTimeout(() => setSaved(false), 2500);
		},
		onError: (e) => {
			setError(getApiErrorMessage(e) ?? t("settings.email.errors.saveFailed"));
		},
	});

	const testMutation = useMutation({
		mutationFn: () => sendTestEmail(testTo.trim()),
		onSuccess: () => {
			setTestMsg({ ok: true, text: t("settings.email.test.success") });
		},
		onError: (e) => {
			setTestMsg({
				ok: false,
				text: getApiErrorMessage(e) ?? t("settings.email.test.failed"),
			});
		},
	});

	return (
		<div className="rounded-xl border border-border/60 bg-card p-6">
			<div className="mb-4 flex items-center gap-2">
				<Mail className="size-4 text-primary" />
				<h3 className="font-[Syne] text-base font-semibold">
					{t("settings.email.title")}
				</h3>
			</div>
			<p className="-mt-2 mb-5 text-sm text-muted-foreground">
				{t("settings.email.description")}
			</p>

			{/* Send-on-create toggle */}
			<div className="mb-6 flex items-start justify-between gap-4 rounded-lg border border-border/60 bg-muted/20 px-4 py-3">
				<div>
					<Label htmlFor="send-on-create" className="font-medium">
						{t("settings.email.sendOnCreateLabel")}
					</Label>
					<p className="mt-0.5 text-xs text-muted-foreground">
						{t("settings.email.sendOnCreateHint")}
					</p>
				</div>
				<Switch
					id="send-on-create"
					checked={sendOnCreate}
					onCheckedChange={setSendOnCreate}
				/>
			</div>

			<div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
				<div className="space-y-1.5">
					<Label htmlFor="smtp-from-email">
						{t("settings.email.fromEmailLabel")}
					</Label>
					<Input
						id="smtp-from-email"
						type="email"
						value={fromEmail}
						onChange={(e) => setFromEmail(e.target.value)}
						placeholder="no-reply@example.com"
					/>
				</div>
				<div className="space-y-1.5">
					<Label htmlFor="smtp-from-name">
						{t("settings.email.fromNameLabel")}
					</Label>
					<Input
						id="smtp-from-name"
						value={fromName}
						onChange={(e) => setFromName(e.target.value)}
						placeholder={t("settings.email.fromNamePlaceholder")}
					/>
				</div>
				<div className="space-y-1.5">
					<Label htmlFor="smtp-host">{t("settings.email.hostLabel")}</Label>
					<Input
						id="smtp-host"
						value={host}
						onChange={(e) => setHost(e.target.value)}
						placeholder="smtp.gmail.com"
					/>
				</div>
				<div className="space-y-1.5">
					<Label htmlFor="smtp-port">{t("settings.email.portLabel")}</Label>
					<Input
						id="smtp-port"
						type="number"
						value={port}
						onChange={(e) => setPort(e.target.value)}
						placeholder="587"
					/>
				</div>
				<div className="space-y-1.5">
					<Label htmlFor="smtp-username">
						{t("settings.email.usernameLabel")}
					</Label>
					<Input
						id="smtp-username"
						value={username}
						onChange={(e) => setUsername(e.target.value)}
						autoComplete="off"
					/>
				</div>
				<div className="space-y-1.5">
					<Label htmlFor="smtp-password">
						{t("settings.email.passwordLabel")}
					</Label>
					<Input
						id="smtp-password"
						type="password"
						value={password}
						onChange={(e) => setPassword(e.target.value)}
						autoComplete="new-password"
						placeholder={
							settings?.password_set
								? t("settings.email.passwordKeepPlaceholder")
								: ""
						}
					/>
				</div>
			</div>

			<div className="mt-4 flex flex-wrap gap-6">
				<div className="flex items-center gap-2 text-sm">
					<Switch id="smtp-ssl" checked={useSSL} onCheckedChange={setUseSSL} />
					<Label htmlFor="smtp-ssl">{t("settings.email.useSSL")}</Label>
				</div>
				<div className="flex items-center gap-2 text-sm">
					<Switch id="smtp-tls" checked={useTLS} onCheckedChange={setUseTLS} />
					<Label htmlFor="smtp-tls">{t("settings.email.useTLS")}</Label>
				</div>
			</div>

			<div className="mt-4 flex items-start justify-between gap-4 rounded-lg border border-amber-500/30 bg-amber-500/5 px-4 py-3">
				<div>
					<Label htmlFor="smtp-skip-verify" className="font-medium">
						{t("settings.email.skipVerifyLabel")}
					</Label>
					<p className="mt-0.5 text-xs text-muted-foreground">
						{t("settings.email.skipVerifyHint")}
					</p>
				</div>
				<Switch
					id="smtp-skip-verify"
					checked={skipVerify}
					onCheckedChange={setSkipVerify}
				/>
			</div>

			{error ? (
				<p className="mt-3 rounded-lg bg-destructive/10 px-3 py-2 text-xs text-destructive">
					{error}
				</p>
			) : null}

			<div className="mt-5 flex items-center gap-2 border-b border-border/50 pb-5">
				<Button
					size="sm"
					disabled={saveMutation.isPending}
					onClick={() => saveMutation.mutate()}
					className="gap-1.5"
				>
					{saveMutation.isPending ? (
						<Loader2 className="size-3.5 animate-spin" />
					) : null}
					{t("settings.email.save")}
				</Button>
				{saved ? (
					<span className="text-xs font-medium text-emerald-600 dark:text-emerald-400">
						{t("settings.email.saved")}
					</span>
				) : null}
			</div>

			{/* Test e-mail */}
			<div className="mt-5">
				<p className="mb-1 text-sm font-medium">
					{t("settings.email.test.title")}
				</p>
				<p className="mb-3 text-xs text-muted-foreground">
					{t("settings.email.test.hint")}
				</p>
				<div className="flex flex-wrap items-center gap-2">
					<Input
						type="email"
						value={testTo}
						onChange={(e) => setTestTo(e.target.value)}
						placeholder={t("settings.email.test.placeholder")}
						className="max-w-xs"
					/>
					<Button
						variant="outline"
						size="sm"
						disabled={testMutation.isPending || testTo.trim() === ""}
						onClick={() => {
							setTestMsg(null);
							testMutation.mutate();
						}}
						className="gap-1.5"
					>
						{testMutation.isPending ? (
							<Loader2 className="size-3.5 animate-spin" />
						) : (
							<Send className="size-3.5" />
						)}
						{t("settings.email.test.button")}
					</Button>
				</div>
				{testMsg ? (
					<p
						className={`mt-2 rounded-lg px-3 py-2 text-xs ${
							testMsg.ok
								? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
								: "bg-destructive/10 text-destructive"
						}`}
					>
						{testMsg.text}
					</p>
				) : null}
			</div>
		</div>
	);
}
