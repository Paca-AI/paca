import { useForm } from "@tanstack/react-form";

type LoginFormValues = {
	username: string;
	password: string;
	rememberMe: boolean;
};

export function useLoginForm() {
	return useForm<LoginFormValues>({
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
