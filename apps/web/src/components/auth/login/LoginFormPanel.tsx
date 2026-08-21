import { useQuery } from "@tanstack/react-query";
import { AlertCircle, Eye, EyeOff } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useBranding } from "@/hooks/use-branding";
import { useLoginForm } from "@/hooks/use-login-form";
import { authConfigQueryOptions } from "@/lib/auth-api";
import { validatePassword, validateUsername } from "@/lib/auth-validation";
import { cn } from "@/lib/utils";

import { FieldError } from "./FieldError";

export function LoginFormPanel() {
	const { t } = useTranslation("auth");
	const { t: tCommon } = useTranslation("common");
	const { form, serverError } = useLoginForm();
	const [showPassword, setShowPassword] = useState(false);
	const branding = useBranding();
	const logoUrl = branding?.logo_thumb_url ?? branding?.logo_url;
	const logoSrc = logoUrl ?? "/paca-logo.svg";
	const brandName = branding?.brand_name;

	// Which login entry points exist on this instance (local password form
	// and/or SSO). While the config loads, keep the familiar local-only
	// layout so the form never flashes away and back.
	const { data: authConfig } = useQuery(authConfigQueryOptions);
	const ssoEnabled = authConfig?.oidc.enabled ?? false;
	const localEnabled = authConfig?.local_login_enabled ?? true;
	const ssoDisplayName =
		authConfig?.oidc.display_name || t("login.ssoDefaultName");

	// The OIDC callback redirects home with ?sso_error=1 on a failed login —
	// the backend deliberately keeps the reason generic.
	const [ssoError] = useState(
		() =>
			typeof window !== "undefined" &&
			new URLSearchParams(window.location.search).get("sso_error") === "1",
	);

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
						{brandName ?? "paca"}
					</span>
				</div>

				{/* Heading */}
				<h1 className="display-title mb-1 text-2xl font-bold text-(--sea-ink) sm:text-3xl">
					{t("login.title")}
				</h1>
				<p className="mb-8 text-sm text-(--sea-ink-soft)">
					{t("login.subtitle")}
				</p>

				{ssoError && (
					<div
						role="alert"
						className="mb-5 flex items-start gap-2.5 rounded-lg border border-red-200 bg-red-50 px-3.5 py-3 text-sm text-red-700 dark:border-red-800/60 dark:bg-red-950/30 dark:text-red-400"
					>
						<AlertCircle className="mt-px size-4 shrink-0" />
						<span>{t("login.errors.ssoFailed")}</span>
					</div>
				)}

				{ssoEnabled && (
					<>
						{/* SSO — the whole OIDC flow runs server-side; the SPA
						    only navigates to the API's login endpoint. */}
						<a
							href="/api/v1/auth/oidc/login"
							className={cn(
								buttonVariants({ size: "lg" }),
								"mb-2 flex h-11 w-full items-center justify-center font-semibold tracking-wide",
							)}
						>
							{t("login.ssoButton", { name: ssoDisplayName })}
						</a>
						{localEnabled && (
							<div className="my-6 flex items-center gap-3">
								<span className="h-px flex-1 bg-(--line)" />
								<span className="text-xs font-medium text-(--sea-ink-soft)/70">
									{t("login.orDivider")}
								</span>
								<span className="h-px flex-1 bg-(--line)" />
							</div>
						)}
					</>
				)}

				{localEnabled && (
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
											onChange={(event) =>
												field.handleChange(event.target.value)
											}
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
				)}

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
