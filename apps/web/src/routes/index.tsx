import { createFileRoute } from "@tanstack/react-router";
import { Eye, EyeOff } from "lucide-react";
import { useState } from "react";

import ThemeToggle from "@/components/ThemeToggle";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useIsDark } from "@/hooks/use-is-dark";
import { useLoginForm } from "@/hooks/use-login-form";
import { markLoginSubmit, setUsernamePreview } from "@/lib/login-store";

export const Route = createFileRoute("/")({ component: App });

function FieldError({ isTouched, error }: { isTouched: boolean; error: string | undefined }) {
	if (!isTouched || !error) return null;
	return (
		<p role="alert" className="text-xs text-red-600 dark:text-red-300">
			{error}
		</p>
	);
}

function App() {
	const form = useLoginForm();
	const [showPassword, setShowPassword] = useState(false);
	const isDark = useIsDark();
	const logoSrc = isDark ? "/paca-logo-dark.svg" : "/paca-logo.svg";

	return (
		<main className="page-wrap px-4 py-10 sm:py-14">
			<div className="mb-4 flex justify-end">
				<ThemeToggle />
			</div>

			<section className="island-shell rise-in relative mx-auto w-full max-w-md overflow-hidden rounded-3xl p-7 sm:p-8">
				<div className="pointer-events-none absolute -left-16 -top-16 h-40 w-40 rounded-full bg-[radial-gradient(circle,rgba(49,95,133,0.26),transparent_65%)]" />
				<div className="pointer-events-none absolute -bottom-16 -right-16 h-40 w-40 rounded-full bg-[radial-gradient(circle,rgba(50,205,50,0.25),transparent_65%)]" />

				<div className="relative">
					<div className="mb-5 flex justify-start">
						<img
							src={logoSrc}
							alt="Paca logo"
							width={127}
							height={175}
							className="h-auto w-12"
						/>
					</div>
					<h1 className="display-title mb-2 text-4xl font-bold text-(--sea-ink)">
						Welcome back
					</h1>
					<p className="mb-7 text-sm text-(--sea-ink-soft)">
						Sign in to continue. This is a UI-only login example.
					</p>

					<form
						onSubmit={(event) => {
							event.preventDefault();
							event.stopPropagation();
							markLoginSubmit();
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
								<div className="space-y-2">
									<Label htmlFor={field.name}>Username</Label>
									<Input
										id={field.name}
										name={field.name}
										type="text"
										autoComplete="username"
										placeholder="Username"
										value={field.state.value}
										onBlur={field.handleBlur}
										onChange={(event) => {
											field.handleChange(event.target.value);
											setUsernamePreview(event.target.value);
										}}
									/>
									<FieldError isTouched={field.state.meta.isTouched} error={field.state.meta.errors[0]!} />
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
								<div className="space-y-2">
									<Label htmlFor={field.name}>Password</Label>
									<div className="relative">
										<Input
											id={field.name}
											name={field.name}
											type={showPassword ? "text" : "password"}
											autoComplete="current-password"
											placeholder="Enter your password"
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(event) =>
												field.handleChange(event.target.value)
											}
											className="pr-10"
										/>
										<button
											type="button"
											onClick={() => setShowPassword((current) => !current)}
											className="absolute right-2.5 top-1/2 -translate-y-1/2 text-(--sea-ink-soft) hover:text-(--sea-ink)"
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
									<FieldError isTouched={field.state.meta.isTouched} error={field.state.meta.errors[0]!} />
								</div>
							)}
						</form.Field>

						<form.Field name="rememberMe">
							{(field) => (
								<div className="flex items-center justify-between rounded-lg border border-(--line) bg-(--chip-bg) px-3 py-2.5">
									<Label htmlFor={field.name}>Remember me</Label>
									<Switch
										id={field.name}
										checked={field.state.value}
										onCheckedChange={field.handleChange}
									/>
								</div>
							)}
						</form.Field>

						<form.Subscribe
							selector={(state) => [state.canSubmit, state.isSubmitting]}
						>
							{([canSubmit, isSubmitting]) => (
								<Button type="submit" className="w-full" disabled={!canSubmit}>
									{isSubmitting ? "Signing in..." : "Sign in"}
								</Button>
							)}
						</form.Subscribe>
					</form>
				</div>
			</section>
		</main>
	);
}
