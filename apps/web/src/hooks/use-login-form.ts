import { useForm } from "@tanstack/react-form";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";

import { currentUserQueryOptions, login } from "@/lib/auth-api";

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
				await login(value.username, value.password);
				await queryClient.invalidateQueries({
					queryKey: currentUserQueryOptions.queryKey,
				});
				await navigate({ to: "/dashboard" });
			} catch (err: unknown) {
				const apiErr = err as {
					response?: { data?: { error?: string } };
				};
				setServerError(
					apiErr?.response?.data?.error ?? "Invalid username or password.",
				);
			}
		},
	});

	return { form, serverError };
}
