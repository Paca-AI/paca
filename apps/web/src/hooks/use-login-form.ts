import { useForm } from "@tanstack/react-form";

export function useLoginForm() {
	return useForm({
		defaultValues: {
			username: "",
			password: "",
			rememberMe: false,
		},
		onSubmit: async ({ value }) => {
			// Demo-only: no auth action yet.
			const { password, ...rest } = value;
			console.info("Login form submitted:", {
				...rest,
				password: "***redacted***",
			});
		},
	});
}
