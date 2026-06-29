import { AlertCircle, Eye, EyeOff } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useLoginForm } from "@/hooks/use-login-form";
import { validatePassword, validateUsername } from "@/lib/auth-validation";
import { cn } from "@/lib/utils";

import { FieldError } from "./FieldError";

export function LoginFormPanel() {
	const { form, serverError } = useLoginForm();
	const { t } = useTranslation();
	const [showPassword, setShowPassword] = useState(false);
	const logoSrc = "/paca-logo.svg";

	return (
		<div className="relative flex flex-col justify-center px-8 py-10 sm:px-10">
			<div className="relative">
				{/* Mobile logo */}
				<div className="mb-7 flex items-center gap-2.5 lg:hidden">
					<img
						src={logoSrc}
						alt="Paca logo"
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
					{t("auth.welcomeBack")}
				</h1>
				<p className="mb-8 text-sm text-(--sea-ink-soft)">
					{t("auth.signInSubtitle")}
				</p>

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
							onBlur: ({ value }) => validateUsername(value),
							onChange: ({ value }) => validateUsername(value),
						}}
					>
						{(field) => (
							<div className="space-y-1.5">
								<Label
									htmlFor={field.name}
									className="text-xs font-semibold tracking-wide text-(--sea-ink) uppercase"
								>
									{t("auth.username")}
								</Label>
								<Input
									id={field.name}
									name={field.name}
									type="text"
									autoComplete="username"
									placeholder={t("auth.usernamePlaceholder")}
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
							onBlur: ({ value }) => validatePassword(value),
							onChange: ({ value }) => validatePassword(value),
						}}
					>
						{(field) => (
							<div className="space-y-1.5">
								<Label
									htmlFor={field.name}
									className="text-xs font-semibold tracking-wide text-(--sea-ink) uppercase"
								>
									{t("auth.password")}
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
												? t("auth.hidePassword")
												: t("auth.showPassword")
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
									{t("auth.keepSignedIn")}
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
								{isSubmitting ? t("auth.signingIn") : t("auth.signIn")}
							</button>
						)}
					</form.Subscribe>
				</form>

				{/* Divider + admin note */}
				<div className="mt-6 border-t border-(--line) pt-5">
					<p className="text-xs leading-relaxed text-(--sea-ink-soft)/70">
						{t("auth.adminNote")}
					</p>
				</div>
			</div>
		</div>
	);
}
