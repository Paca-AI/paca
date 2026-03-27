import { AlertCircle, Eye, EyeOff } from "lucide-react";
import { useState } from "react";

import { buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useIsDark } from "@/hooks/use-is-dark";
import { useLoginForm } from "@/hooks/use-login-form";
import { cn } from "@/lib/utils";

import { FieldError } from "./FieldError";

export function LoginFormPanel() {
	const { form, serverError } = useLoginForm();
	const [showPassword, setShowPassword] = useState(false);
	const isDark = useIsDark();
	const logoSrc = isDark ? "/paca-logo-dark.svg" : "/paca-logo.svg";

	return (
		<div className="relative flex flex-col justify-center px-8 py-10 sm:px-10">
			{/* Top accent line */}
			<div className="pointer-events-none absolute inset-x-0 top-0 h-px bg-[linear-gradient(90deg,transparent,rgba(50,205,50,0.45),transparent)] lg:hidden" />
			<div className="pointer-events-none absolute -right-10 -top-10 h-36 w-36 rounded-full bg-[radial-gradient(circle,rgba(50,205,50,0.14),transparent_65%)] lg:hidden" />

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
				<h1 className="display-title mb-1 text-2xl font-bold text-(--sea-ink) sm:text-[1.75rem]">
					Welcome back
				</h1>
				<p className="mb-8 text-sm text-(--sea-ink-soft)">
					Sign in to your workspace to continue.
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
							onBlur: ({ value }) => {
								if (!value.trim()) {
									return "Username is required";
								}
								if (value.trim().length < 3) {
									return "Username must be at least 3 characters";
								}
								return undefined;
							},
						}}
					>
						{(field) => (
							<div className="space-y-1.5">
								<Label
									htmlFor={field.name}
									className="text-xs font-semibold tracking-wide text-(--sea-ink) uppercase"
								>
									Username
								</Label>
								<Input
									id={field.name}
									name={field.name}
									type="text"
									autoComplete="username"
									placeholder="Enter your username"
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
							onBlur: ({ value }) => {
								if (!value.trim()) {
									return "Password is required";
								}
								if (value.length < 8) {
									return "Password must be at least 8 characters";
								}
								return undefined;
							},
						}}
					>
						{(field) => (
							<div className="space-y-1.5">
								<Label
									htmlFor={field.name}
									className="text-xs font-semibold tracking-wide text-(--sea-ink) uppercase"
								>
									Password
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
											showPassword ? "Hide password" : "Show password"
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
									Keep me signed in
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
									"mt-1 h-11 w-full font-semibold tracking-wide",
								)}
								style={{
									background: isSubmitting
										? undefined
										: "linear-gradient(135deg, #2e4980 0%, #1b3360 100%)",
								}}
								disabled={isSubmitting || !username.trim() || !password}
							>
								{isSubmitting ? "Signing in…" : "Sign in"}
							</button>
						)}
					</form.Subscribe>
				</form>

				{/* Divider + admin note */}
				<div className="mt-6 border-t border-(--line) pt-5">
					<p className="text-xs leading-relaxed text-(--sea-ink-soft)/70">
						Account access and password resets are managed by your
						administrator.
					</p>
				</div>
			</div>
		</div>
	);
}
