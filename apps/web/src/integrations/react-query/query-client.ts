import { QueryClient } from "@tanstack/react-query";

function getHttpStatus(error: unknown): number | undefined {
	return (error as { response?: { status?: number } } | undefined)?.response
		?.status;
}

/**
 * Skips retrying 4xx responses (401/403/404/etc.) — those mean the request
 * was rejected for a reason another attempt won't fix (missing permission,
 * missing resource, bad input), unlike a 5xx or network failure, which might
 * genuinely succeed later. Without this, every permission-denied ("403
 * FORBIDDEN") query used React Query's default retry (3 attempts with
 * exponential backoff), so components kept hitting the API and sitting in
 * a loading state for several seconds before finally reaching their error
 * state — same request outcome, several seconds later.
 *
 * Exported for testing.
 */
export function shouldRetryQuery(
	failureCount: number,
	error: unknown,
): boolean {
	const status = getHttpStatus(error);
	if (status !== undefined && status >= 400 && status < 500) {
		return false;
	}
	return failureCount < 3;
}

export const queryClient = new QueryClient({
	defaultOptions: {
		queries: {
			retry: shouldRetryQuery,
		},
	},
});
