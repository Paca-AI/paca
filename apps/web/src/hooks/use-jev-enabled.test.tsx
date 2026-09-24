import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

const { fetchProject } = vi.hoisted(() => ({ fetchProject: vi.fn() }));

vi.mock("@/lib/project-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/project-api")>(
			"@/lib/project-api",
		);
	return {
		...actual,
		projectQueryOptions: (projectId: string) => ({
			queryKey: ["projects", projectId],
			queryFn: () => fetchProject(projectId),
		}),
	};
});

import { useJevEnabled } from "./use-jev-enabled";

function wrapper() {
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return ({ children }: { children: ReactNode }) => (
		<QueryClientProvider client={client}>{children}</QueryClientProvider>
	);
}

describe("useJevEnabled", () => {
	it("is true when the project has Jev configured", async () => {
		fetchProject.mockResolvedValue({ id: "p1", jev_configured: true });
		const { result } = renderHook(() => useJevEnabled("p1"), {
			wrapper: wrapper(),
		});
		await waitFor(() => expect(result.current).toBe(true));
	});

	it("is false when the project has not configured Jev", async () => {
		fetchProject.mockResolvedValue({ id: "p2", jev_configured: false });
		const { result } = renderHook(() => useJevEnabled("p2"), {
			wrapper: wrapper(),
		});
		await waitFor(() => expect(fetchProject).toHaveBeenCalledWith("p2"));
		expect(result.current).toBe(false);
	});

	it("is false and never fetches without a project", () => {
		fetchProject.mockClear();
		const { result } = renderHook(() => useJevEnabled(undefined), {
			wrapper: wrapper(),
		});
		expect(result.current).toBe(false);
		expect(fetchProject).not.toHaveBeenCalled();
	});

	it("is false while the project is loading or fails to load", async () => {
		fetchProject.mockRejectedValue(new Error("nope"));
		const { result } = renderHook(() => useJevEnabled("p3"), {
			wrapper: wrapper(),
		});
		expect(result.current).toBe(false);
		await waitFor(() => expect(fetchProject).toHaveBeenCalledWith("p3"));
		expect(result.current).toBe(false);
	});
});
