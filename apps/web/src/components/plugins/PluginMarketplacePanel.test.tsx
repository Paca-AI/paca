import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const {
	mockListMarketplacePlugins,
	mockListPlugins,
	mockInstallMarketplacePlugin,
	mockUninstallPlugin,
	mockUpgradePlugin,
} = vi.hoisted(() => ({
	mockListMarketplacePlugins: vi.fn(),
	mockListPlugins: vi.fn(),
	mockInstallMarketplacePlugin: vi.fn(),
	mockUninstallPlugin: vi.fn(),
	mockUpgradePlugin: vi.fn(),
}));

vi.mock("@/lib/plugin-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/plugin-api")>(
			"@/lib/plugin-api",
		);
	return {
		...actual,
		listMarketplacePlugins: mockListMarketplacePlugins,
		listPlugins: mockListPlugins,
		installMarketplacePlugin: mockInstallMarketplacePlugin,
		uninstallPlugin: mockUninstallPlugin,
		upgradePlugin: mockUpgradePlugin,
		marketplacePluginsQueryOptions: {
			queryKey: ["plugins", "marketplace"],
			queryFn: mockListMarketplacePlugins,
			retry: false,
		},
		pluginsQueryOptions: {
			queryKey: ["plugins"],
			queryFn: mockListPlugins,
			retry: false,
		},
	};
});

import type { MarketplacePlugin } from "@/lib/plugin-api";
import { PluginMarketplacePanel } from "./PluginMarketplacePanel";

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeQueryClient() {
	return new QueryClient({
		defaultOptions: {
			mutations: { retry: false },
			queries: { retry: false, gcTime: 0 },
		},
	});
}

function Wrapper({ children }: { children: ReactNode }) {
	return (
		<QueryClientProvider client={makeQueryClient()}>
			{children}
		</QueryClientProvider>
	);
}

const examplePlugin: MarketplacePlugin = {
	name: "com.paca.example",
	display_name: "Plugin SDK Hello World",
	description: "Hello world examples for every Paca plugin SDK feature.",
	version: "0.1.0",
	artifacts: {
		manifest_tar_gz_url: "https://example.com/manifest.tar.gz",
	},
};

function renderPanel() {
	render(
		<Wrapper>
			<PluginMarketplacePanel />
		</Wrapper>,
	);
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe("PluginMarketplacePanel", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockListMarketplacePlugins.mockResolvedValue([examplePlugin]);
		mockListPlugins.mockResolvedValue([]);
	});

	it("installs a plugin when Install is clicked", async () => {
		mockInstallMarketplacePlugin.mockResolvedValue({});
		renderPanel();

		await screen.findByText("Plugin SDK Hello World");
		await userEvent.click(screen.getByRole("button", { name: /install/i }));

		await waitFor(() => {
			expect(mockInstallMarketplacePlugin).toHaveBeenCalled();
		});
		expect(mockInstallMarketplacePlugin.mock.calls[0][0]).toEqual({
			name: "com.paca.example",
			enabled: true,
		});
	});

	it("shows a translated, interpolated message when install fails due to an incompatible host version", async () => {
		mockInstallMarketplacePlugin.mockRejectedValue({
			response: {
				data: {
					error_code: "PLUGIN_INCOMPATIBLE_HOST_VERSION",
					error:
						'plugin "com.paca.example" requires Paca v0.11.2 or later (running v0.10.0)',
					error_details: {
						plugin_id: "com.paca.example",
						required_version: "v0.11.2",
						host_version: "v0.10.0",
					},
				},
			},
		});
		renderPanel();

		await screen.findByText("Plugin SDK Hello World");
		await userEvent.click(screen.getByRole("button", { name: /install/i }));

		await waitFor(() => {
			expect(
				screen.getByText(
					"This plugin requires Paca v0.11.2 or later — you're running v0.10.0.",
				),
			).toBeInTheDocument();
		});
	});

	it("falls back to the generic incompatible-version message when error_details is missing", async () => {
		mockInstallMarketplacePlugin.mockRejectedValue({
			response: {
				data: {
					error_code: "PLUGIN_INCOMPATIBLE_HOST_VERSION",
					error: "plugin requires a newer Paca version",
				},
			},
		});
		renderPanel();

		await screen.findByText("Plugin SDK Hello World");
		await userEvent.click(screen.getByRole("button", { name: /install/i }));

		await waitFor(() => {
			expect(
				screen.getByText(
					"This plugin requires a newer version of Paca than you're running.",
				),
			).toBeInTheDocument();
		});
	});

	it("shows a generic message when install fails without a recognizable error code", async () => {
		mockInstallMarketplacePlugin.mockRejectedValue(new Error("network down"));
		renderPanel();

		await screen.findByText("Plugin SDK Hello World");
		await userEvent.click(screen.getByRole("button", { name: /install/i }));

		await waitFor(() => {
			expect(
				screen.getByText("Something went wrong. Please try again."),
			).toBeInTheDocument();
		});
	});

	it("clears a previous install error once a retry succeeds", async () => {
		const errorText =
			"This plugin requires a newer version of Paca than you're running.";
		mockInstallMarketplacePlugin.mockRejectedValueOnce({
			response: {
				data: {
					error_code: "PLUGIN_INCOMPATIBLE_HOST_VERSION",
					error: "plugin requires a newer Paca version",
				},
			},
		});
		renderPanel();

		await screen.findByText("Plugin SDK Hello World");
		await userEvent.click(screen.getByRole("button", { name: /install/i }));
		await screen.findByText(errorText);

		mockInstallMarketplacePlugin.mockResolvedValueOnce({});
		await userEvent.click(screen.getByRole("button", { name: /install/i }));

		await waitFor(() => {
			expect(screen.queryByText(errorText)).not.toBeInTheDocument();
		});
	});
});
