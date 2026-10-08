import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@tanstack/react-router", () => ({
	Link: ({ children, to }: { children: ReactNode; to: string }) => (
		<a href={to}>{children}</a>
	),
}));

const { mockListPlugins } = vi.hoisted(() => ({ mockListPlugins: vi.fn() }));

vi.mock("@/lib/plugin-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/plugin-api")>(
			"@/lib/plugin-api",
		);
	return {
		...actual,
		pluginsQueryOptions: {
			queryKey: ["plugins"],
			queryFn: mockListPlugins,
			retry: false,
		},
	};
});

import { renderWithQueries } from "@/test/render-with-queries";
import { LegacyPluginsBanner } from "./LegacyPluginsBanner";

const plugin = (name: string, displayName: string, legacy: boolean) => ({
	id: name,
	name,
	version: "1.0.0",
	manifest: { id: name, displayName, version: "1.0.0" },
	enabled: true,
	legacy_permissions: legacy,
	installed_at: "2026-01-01T00:00:00.000Z",
	updated_at: "2026-01-01T00:00:00.000Z",
});

beforeEach(() => {
	vi.clearAllMocks();
	window.localStorage.clear();
});

describe("LegacyPluginsBanner", () => {
	it("names the installed plugins that still use requirePermissions", async () => {
		mockListPlugins.mockResolvedValue([
			plugin("a", "Alpha", true),
			plugin("b", "Beta", false),
			plugin("c", "Gamma", true),
		]);
		renderWithQueries(<LegacyPluginsBanner />, {
			permissions: ["plugins:write"],
		});

		const alert = await screen.findByRole("alert");
		expect(alert).toHaveTextContent("Plugins need an update");
		expect(alert).toHaveTextContent("Alpha, Gamma");
		expect(alert).not.toHaveTextContent("Beta");
		expect(
			screen.getByRole("link", { name: /manage plugins/i }),
		).toHaveAttribute("href", "/admin/plugins");
	});

	it("uses the singular for one plugin", async () => {
		mockListPlugins.mockResolvedValue([plugin("a", "Alpha", true)]);
		renderWithQueries(<LegacyPluginsBanner />, {
			permissions: ["plugins:write"],
		});

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"Plugin needs an update",
		);
	});

	it("is not shown when no plugin uses it", async () => {
		mockListPlugins.mockResolvedValue([plugin("b", "Beta", false)]);
		renderWithQueries(<LegacyPluginsBanner />, {
			permissions: ["plugins:write"],
		});

		await vi.waitFor(() => expect(mockListPlugins).toHaveBeenCalled());
		expect(screen.queryByRole("alert")).not.toBeInTheDocument();
	});

	it("is only for people who can manage plugins", () => {
		mockListPlugins.mockResolvedValue([plugin("a", "Alpha", true)]);
		renderWithQueries(<LegacyPluginsBanner />, { permissions: ["tasks:read"] });

		expect(mockListPlugins).not.toHaveBeenCalled();
		expect(screen.queryByRole("alert")).not.toBeInTheDocument();
	});

	it("stays dismissed for the same plugins and returns for a newly affected one", async () => {
		mockListPlugins.mockResolvedValue([plugin("a", "Alpha", true)]);
		const view = renderWithQueries(<LegacyPluginsBanner />, {
			permissions: ["plugins:write"],
		});
		await userEvent.click(
			await screen.findByRole("button", { name: "Dismiss" }),
		);
		expect(screen.queryByRole("alert")).not.toBeInTheDocument();

		view.unmount();
		renderWithQueries(<LegacyPluginsBanner />, {
			permissions: ["plugins:write"],
		});
		await vi.waitFor(() => expect(mockListPlugins).toHaveBeenCalledTimes(2));
		expect(screen.queryByRole("alert")).not.toBeInTheDocument();

		mockListPlugins.mockResolvedValue([
			plugin("a", "Alpha", true),
			plugin("c", "Gamma", true),
		]);
		const again = renderWithQueries(<LegacyPluginsBanner />, {
			permissions: ["plugins:write"],
		});
		expect(await screen.findByRole("alert")).toHaveTextContent("Alpha, Gamma");
		again.unmount();
	});
});
