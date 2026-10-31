import { useForm } from "@tanstack/react-form";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { login } from "@/lib/auth-api";
import { savePendingMfa } from "@/lib/mfa-challenge";

function loginErrorMessage(
	code: ApiErrorCode | null,
	t: TFunction<"auth">,
): string {
	if (code === ApiErrorCode.InvalidCredentials) {
		return t("login.errors.invalidCredentials");
	}
	if (code === ApiErrorCode.Unauthenticated) {
		return t("login.errors.sessionExpired");
	}
	if (code === ApiErrorCode.IpNotAllowed) {
		return t("login.errors.ipNotAllowed");
	}
	if (code === ApiErrorCode.TooManyRequests) {
		return t("login.errors.tooManyAttempts");
	}
	return t("login.errors.genericError");
}

/**
 * `redirectTo` is a same-origin path (already sanitized by the route) to open
 * after signing in instead of the home page.
 */
export function useLoginForm(redirectTo?: string) {
	const { t } = useTranslation("auth");
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const [serverError, setServerError] = useState<string | null>(null);
	// Set when policy requires two-step verification the account hasn't set
	// up: the login panel runs enrollment with these credentials, then signs
	// in again, which lands on the code page.
	const [mfaSetup, setMfaSetup] = useState<{
		username: string;
		password: string;
		rememberMe: boolean;
	} | null>(null);

	/** Password sign-in. Opens the app, or the code page when a second factor is owed. */
	async function signIn(
		username: string,
		password: string,
		rememberMe: boolean,
	): Promise<void> {
		const challenge = await login(username, password, rememberMe);
		if (challenge) {
			savePendingMfa({ challenge, redirectTo });
			await navigate({ to: "/login/mfa" });
			return;
		}
		// Invalidate the entire "auth" query namespace so the ("auth"/"me")
		// cache is refreshed. Without this the sidebar keeps the previous
		// user's data.
		await queryClient.invalidateQueries({ queryKey: ["auth"] });
		if (redirectTo) {
			// A full load, so the destination starts from the fresh session.
			window.location.assign(redirectTo);
			return;
		}
		await navigate({ to: "/home" });
	}

	const form = useForm({
		defaultValues: {
			username: "",
			password: "",
			rememberMe: false,
		},
		onSubmit: async ({ value }) => {
			setServerError(null);
			try {
				await signIn(value.username, value.password, value.rememberMe);
			} catch (err: unknown) {
				const code = getApiErrorCode(err);
				if (code === ApiErrorCode.MfaSetupRequired) {
					setMfaSetup({
						username: value.username,
						password: value.password,
						rememberMe: value.rememberMe,
					});
					return;
				}
				setServerError(loginErrorMessage(code, t));
			}
		},
	});

	return {
		form,
		serverError,
		mfaSetup,
		/** Enrollment done: sign in again, which now owes (and asks for) a code. */
		finishMfaSetup: async () => {
			if (!mfaSetup) return;
			const { username, password, rememberMe } = mfaSetup;
			setMfaSetup(null);
			try {
				await signIn(username, password, rememberMe);
			} catch (err: unknown) {
				setServerError(loginErrorMessage(getApiErrorCode(err), t));
			}
		},
	};
}
