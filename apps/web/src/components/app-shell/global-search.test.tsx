import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ComponentProps, ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { SearchResults } from "@/lib/search-api";
import { useGlobalSearchStore } from "@/lib/shortcuts/global-search-store";

const mocks = vi.hoisted(() => ({
	navigateMock: vi.fn(),
	searchMock: vi.fn(),
	setOpenMobileMock: vi.fn(),
	permissions: [] as string[],
}));

vi.mock("@tanstack/react-router", () => ({
	useNavigate: () => mocks.navigateMock,
}));

vi.mock("@/components/ui/sidebar", () => ({
	SidebarMenu: ({ children }: { children: ReactNode }) => <ul>{children}</ul>,
	SidebarMenuItem: ({ children }: { children: ReactNode }) => (
		<li>{children}</li>
	),
	SidebarMenuButton: ({
		tooltip: _tooltip,
		...props
	}: ComponentProps<"button"> & { tooltip?: string }) => (
		<button type="button" {...props} />
	),
	useSidebar: () => ({ setOpenMobile: mocks.setOpenMobileMock }),
}));

vi.mock("@/hooks/use-permissions", () => ({
	usePermissions: () => ({
		hasPermission: (a: string) => mocks.permissions.includes(a),
	}),
}));

vi.mock("@/lib/search-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/search-api")>(
			"@/lib/search-api",
		);
	return {
		...actual,
		searchQueryOptions: (q: string) => ({
			...actual.searchQueryOptions(q),
			queryFn: () => mocks.searchMock(q),
		}),
	};
});

import { GlobalSearchButton, GlobalSearchDialog } from "./global-search";

// cmdk measures its list and scrolls the active item into view; jsdom has
// neither API.
globalThis.ResizeObserver ??= class {
	observe() {}
	unobserve() {}
	disconnect() {}
};
Element.prototype.scrollIntoView ??= () => {};

const RESULTS: SearchResults = {
	query: "log",
	projects: [
		{
			id: "p1",
			name: "Logistics",
			description: "",
			task_id_prefix: "lgx",
		},
	],
	tasks: [
		{
			id: "t1",
			project_id: "p1",
			project_name: "Logistics",
			task_key: "LGX-12",
			task_number: 12,
			title: "Fix login page",
			status_name: "To Do",
			status_category: "todo",
			type_name: "Epic",
			type_icon: "Layers",
			type_color: "#8b5cf6",
		},
		{
			id: "t2",
			project_id: "p1",
			project_name: "Logistics",
			task_key: "LGX-13",
			task_number: 13,
			title: "Logging story",
			type_name: "Story",
			type_color: "#22c55e",
		},
	],
	people: [
		{ id: "u1", name: "Logan", username: "logan", project_id: "p1" },
		{ id: "u2", name: "Lois", username: "lois" },
	],
};

function wrapper({ children }: { children: ReactNode }) {
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function GlobalSearch() {
	return (
		<>
			<GlobalSearchButton />
			<GlobalSearchDialog />
		</>
	);
}

async function openAndType(text: string) {
	const user = userEvent.setup();
	render(<GlobalSearch />, { wrapper });
	await user.click(screen.getByRole("button", { name: "Search" }));
	await user.type(await screen.findByRole("combobox"), text);
	return user;
}

describe("GlobalSearch", () => {
	beforeEach(() => {
		mocks.navigateMock.mockReset();
		mocks.searchMock.mockReset();
		mocks.setOpenMobileMock.mockReset();
		mocks.permissions = [];
		act(() => useGlobalSearchStore.getState().setOpen(false));
	});

	it("shows a hint before anything is typed and does not search", async () => {
		const user = userEvent.setup();
		render(<GlobalSearch />, { wrapper });
		await user.click(screen.getByRole("button", { name: "Search" }));
		expect(
			await screen.findByText(/Type to search your projects/),
		).toBeInTheDocument();
		expect(mocks.searchMock).not.toHaveBeenCalled();
	});

	it("closes the mobile sidebar when opened from it", async () => {
		const user = userEvent.setup();
		render(<GlobalSearch />, { wrapper });
		await user.click(screen.getByRole("button", { name: "Search" }));
		expect(mocks.setOpenMobileMock).toHaveBeenCalledWith(false);
		expect(await screen.findByRole("combobox")).toBeInTheDocument();
	});

	it("groups the results the API returns and opens a task", async () => {
		mocks.searchMock.mockResolvedValue(RESULTS);
		const user = await openAndType("log");

		await screen.findByText("LGX-12");
		expect(mocks.searchMock).toHaveBeenLastCalledWith("log");
		expect(screen.getByText("Tasks")).toBeInTheDocument();
		expect(screen.getByText("Projects")).toBeInTheDocument();
		expect(screen.getByText("People")).toBeInTheDocument();
		expect(screen.getByText("Logistics · To Do")).toBeInTheDocument();
		expect(screen.getByText("LGX")).toBeInTheDocument();

		await user.click(screen.getByText("LGX-12"));
		expect(mocks.navigateMock).toHaveBeenCalledWith({
			to: "/projects/$projectId/tasks/$taskId",
			params: { projectId: "p1", taskId: "t1" },
		});
		expect(useGlobalSearchStore.getState().open).toBe(false);
	});

	it("highlights the first result so Enter opens it", async () => {
		mocks.searchMock.mockResolvedValue(RESULTS);
		const user = await openAndType("log");
		const first = (await screen.findByText("LGX-12")).closest(
			"[data-slot=command-item]",
		);
		await waitFor(() => expect(first).toHaveAttribute("data-selected", "true"));
		await user.keyboard("{Enter}");
		expect(mocks.navigateMock).toHaveBeenCalledWith({
			to: "/projects/$projectId/tasks/$taskId",
			params: { projectId: "p1", taskId: "t1" },
		});
	});

	it("draws each task's type in the project's color", async () => {
		mocks.searchMock.mockResolvedValue(RESULTS);
		await openAndType("log");

		// A type with an icon: the icon, tinted.
		const epic = await screen.findByRole("img", { name: "Epic" });
		expect(epic).toHaveStyle({ color: "#8b5cf6" });
		// A type without one: its name in a tinted pill.
		expect(screen.getByText("Story")).toHaveStyle({ color: "#22c55e" });
	});

	it("opens a teammate's team page", async () => {
		mocks.searchMock.mockResolvedValue(RESULTS);
		const user = await openAndType("log");

		await user.click(await screen.findByText("@logan"));
		expect(mocks.navigateMock).toHaveBeenCalledWith({
			to: "/projects/$projectId/team",
			params: { projectId: "p1" },
		});
	});

	it("only lets a user admin open someone outside their teams", async () => {
		mocks.searchMock.mockResolvedValue(RESULTS);
		const user = await openAndType("log");
		const lois = (await screen.findByText("@lois")).closest(
			"[data-slot=command-item]",
		);
		expect(lois).toHaveAttribute("data-disabled", "true");
		await user.click(screen.getByText("@lois"));
		expect(mocks.navigateMock).not.toHaveBeenCalled();
	});

	it("says so when nothing matches", async () => {
		mocks.searchMock.mockResolvedValue({
			query: "zzz",
			projects: [],
			tasks: [],
			people: [],
		});
		await openAndType("zzz");
		await waitFor(() =>
			expect(screen.getByText("No results for “zzz”")).toBeInTheDocument(),
		);
	});
});
