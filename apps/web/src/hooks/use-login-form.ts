import { useForm } from "@tanstack/react-form";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { currentUserQueryOptions, login } from "@/lib/auth-api";

const loginErrorMessages: Partial<Record<ApiErrorCode, string>> = {
	[ApiErrorCode.InvalidCredentials]: "Invalid username or password.",
	[ApiErrorCode.Unauthenticated]: "Session expired. Please log in again.",
};

export function useLoginForm() {
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const [serverError, setServerError] = useState<string | null>(null);

	const form = useForm({
		defaultValues: {
			username: "",
			password: "",
			rememberMe: false,
		},
		onSubmit: async ({ value }) => {
			setServerError(null);
			try {
				await login(value.username, value.password, value.rememberMe);
				await queryClient.invalidateQueries({
					queryKey: currentUserQueryOptions.queryKey,
				});
				await navigate({ to: "/dashboard" });
			} catch (err: unknown) {
				const code = getApiErrorCode(err);
				setServerError(
					(code && loginErrorMessages[code]) ??
						"Something went wrong. Please try again.",
				);
			}
		},
	});

	return { form, serverError };
}
