import { useQuery } from "@tanstack/react-query";
import { AlertCircle, Eye, EyeOff, KeyRound } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useLoginForm } from "@/hooks/use-login-form";
import { authConfigQueryOptions } from "@/lib/auth-api";
import { validatePassword, validateUsername } from "@/lib/auth-validation";
import { cn } from "@/lib/utils";

import { FieldError } from "./FieldError";

/** Browser-navigation target for the OIDC SSO flow (full redirect, not XHR). */
const OIDC_LOGIN_URL = "/api/v1/auth/oidc/login";

export function LoginFormPanel() {
	const { t } = useTranslation("auth");
	const { t: tCommon } = useTranslation("common");
	const { form, serverError } = useLoginForm();
	const [showPassword, setShowPassword] = useState(false);
	const { data: authConfig, isLoading: authConfigLoading } = useQuery(
		authConfigQueryOptions,
	);
	const logoSrc = "/paca-logo.svg";

	// ADR-038 T2 — platform-wide design: apps do NOT show their own login
	// screen. When the server enables OIDC we bounce straight to Vortex SSO;
	// the local username/password form stays reachable only via ?local=1
	// (admin break-glass). If the config fetch fails, the form renders as a
	// fallback so a broken identity service never locks out break-glass.
	const forceLocal =
		typeof window !== "undefined" &&
		new URLSearchParams(window.location.search).get("local") === "1";
	const ssoRedirect = Boolean(authConfig?.oidc_enabled) && !forceLocal;
	useEffect(() => {
		if (ssoRedirect) window.location.replace(OIDC_LOGIN_URL);
	}, [ssoRedirect]);

	// Avoid flashing the local form while the auth config is still loading.
	if (authConfigLoading) return null;

	if (ssoRedirect) {
		return (
			<div className="relative flex flex-col items-center justify-center gap-4 px-8 py-16 sm:px-10">
				<img src={logoSrc} alt={t("brand.logoAlt")} className="h-auto w-10" />
				<a href={OIDC_LOGIN_URL} className={cn(buttonVariants({ size: "lg" }))}>
					{authConfig?.oidc_button_label || t("login.ssoSignIn")}
				</a>
			</div>
		);
	}

	return (
		<div className="relative flex flex-col justify-center px-8 py-10 sm:px-10">
			<div className="relative">
				{/* Mobile logo */}
				<div className="mb-7 flex items-center gap-2.5 lg:hidden">
					<img
						src={logoSrc}
						alt={t("brand.logoAlt")}
						width={127}
						height={175}
						className="h-auto w-8"
					/>
					<span className="text-base font-bold tracking-tight text-(--sea-ink)">
						paca
					</span>
				</div>

				{/* Heading */}
				<h1 className="display-title mb-1 text-2xl font-bold text-(--sea-ink) sm:text-3xl">
					{t("login.title")}
				</h1>
				<p className="mb-8 text-sm text-(--sea-ink-soft)">
					{t("login.subtitle")}
				</p>

				{/* OIDC SSO (ADR-038) — primary path when the server enables it;
				    username/password below stays available as break-glass. */}
				{authConfig?.oidc_enabled && (
					<div className="mb-6">
						<a
							href={OIDC_LOGIN_URL}
							className={cn(
								buttonVariants({ size: "lg" }),
								"h-11 w-full font-semibold tracking-wide bg-primary text-primary-foreground hover:bg-primary/90",
							)}
						>
							<KeyRound className="size-4" />
							{authConfig.oidc_button_label || t("login.ssoSignIn")}
						</a>
						<div className="mt-6 flex items-center gap-3">
							<span className="h-px flex-1 bg-(--line)" />
							<span className="text-xs tracking-wide text-(--sea-ink-soft)/70 uppercase">
								{t("login.ssoDivider")}
							</span>
							<span className="h-px flex-1 bg-(--line)" />
						</div>
					</div>
				)}

				<form
					onSubmit={(event) => {
						event.preventDefault();
						event.stopPropagation();
						form.handleSubmit();
					}}
					className="space-y-5"
				>
					<form.Field
						name="username"
						validators={{
							onBlur: ({ value }) => validateUsername(value, tCommon),
							onChange: ({ value }) => validateUsername(value, tCommon),
						}}
					>
						{(field) => (
							<div className="space-y-1.5">
								<Label
									htmlFor={field.name}
									className="text-xs font-semibold tracking-wide text-(--sea-ink) uppercase"
								>
									{t("login.usernameLabel")}
								</Label>
								<Input
									id={field.name}
									name={field.name}
									type="text"
									autoComplete="username"
									placeholder={t("login.usernamePlaceholder")}
									value={field.state.value}
									onBlur={field.handleBlur}
									onChange={(event) => {
										field.handleChange(event.target.value);
									}}
									className="h-10"
								/>
								<FieldError
									isTouched={field.state.meta.isTouched}
									error={field.state.meta.errors[0]}
								/>
							</div>
						)}
					</form.Field>

					<form.Field
						name="password"
						validators={{
							onBlur: ({ value }) => validatePassword(value, tCommon),
							onChange: ({ value }) => validatePassword(value, tCommon),
						}}
					>
						{(field) => (
							<div className="space-y-1.5">
								<Label
									htmlFor={field.name}
									className="text-xs font-semibold tracking-wide text-(--sea-ink) uppercase"
								>
									{t("login.passwordLabel")}
								</Label>
								<div className="relative">
									<Input
										id={field.name}
										name={field.name}
										type={showPassword ? "text" : "password"}
										autoComplete="current-password"
										placeholder="••••••••"
										value={field.state.value}
										onBlur={field.handleBlur}
										onChange={(event) => field.handleChange(event.target.value)}
										className="h-10 pr-10"
									/>
									<button
										type="button"
										onClick={() => setShowPassword((current) => !current)}
										className="absolute right-2.5 top-1/2 -translate-y-1/2 rounded p-0.5 text-(--sea-ink-soft) transition-colors hover:text-(--sea-ink)"
										aria-label={
											showPassword
												? t("login.hidePassword")
												: t("login.showPassword")
										}
									>
										{showPassword ? (
											<EyeOff className="size-4" />
										) : (
											<Eye className="size-4" />
										)}
									</button>
								</div>
								<FieldError
									isTouched={field.state.meta.isTouched}
									error={field.state.meta.errors[0]}
								/>
							</div>
						)}
					</form.Field>

					{serverError && (
						<div
							role="alert"
							className="flex items-start gap-2.5 rounded-lg border border-red-200 bg-red-50 px-3.5 py-3 text-sm text-red-700 dark:border-red-800/60 dark:bg-red-950/30 dark:text-red-400"
						>
							<AlertCircle className="mt-px size-4 shrink-0" />
							<span>{serverError}</span>
						</div>
					)}

					<form.Field name="rememberMe">
						{(field) => (
							<div className="flex items-center justify-between">
								<Label
									htmlFor={field.name}
									className="cursor-pointer text-sm text-(--sea-ink-soft)"
								>
									{t("login.rememberMe")}
								</Label>
								<Switch
									id={field.name}
									checked={field.state.value}
									onCheckedChange={field.handleChange}
								/>
							</div>
						)}
					</form.Field>

					<form.Subscribe
						selector={(state) => ({
							username: state.values.username,
							password: state.values.password,
							isSubmitting: state.isSubmitting,
						})}
					>
						{({ username, password, isSubmitting }) => (
							<button
								type="submit"
								className={cn(
									buttonVariants({ size: "lg" }),
									"mt-1 h-11 w-full font-semibold tracking-wide bg-primary text-primary-foreground hover:bg-primary/90",
								)}
								disabled={isSubmitting || !username.trim() || !password}
							>
								{isSubmitting ? t("login.signingIn") : t("login.signIn")}
							</button>
						)}
					</form.Subscribe>
				</form>

				{/* Divider + admin note */}
				<div className="mt-6 border-t border-(--line) pt-5">
					<p className="text-xs leading-relaxed text-(--sea-ink-soft)/70">
						{t("login.adminManagedNote")}
					</p>
				</div>
			</div>
		</div>
	);
}
