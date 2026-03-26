type FieldErrorProps = {
	isTouched: boolean;
	error: string | undefined;
};

export function FieldError({ isTouched, error }: FieldErrorProps) {
	if (!isTouched || !error) {
		return null;
	}

	return (
		<p role="alert" className="mt-1 text-xs text-red-600 dark:text-red-400">
			{error}
		</p>
	);
}
