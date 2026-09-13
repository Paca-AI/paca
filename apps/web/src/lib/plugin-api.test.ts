import { describe, expect, it } from "vitest";
import { buildRegistryMap, type Plugin } from "./plugin-api";

function makePlugin(overrides: Partial<Plugin> = {}): Plugin {
	return {
		id: "uuid-1",
		name: "com.paca.example",
		version: "1.0.0",
		enabled: true,
		installed_at: "2026-01-01T00:00:00Z",
		updated_at: "2026-01-01T00:00:00Z",
		manifest: {
			id: "com.paca.example",
			displayName: "Example",
			version: "1.0.0",
			frontend: {
				remoteEntryUrl: "https://example.test/remoteEntry.js",
				extensionPoints: [
					{ point: "project.settings.tab", component: "SettingsTab" },
				],
			},
		},
		...overrides,
	};
}

describe("buildRegistryMap", () => {
	it("carries requiredPermission through from the manifest to the registration", () => {
		const plugin = makePlugin({
			manifest: {
				id: "com.paca.example",
				displayName: "Example",
				version: "1.0.0",
				frontend: {
					remoteEntryUrl: "https://example.test/remoteEntry.js",
					extensionPoints: [
						{
							point: "project.settings.tab",
							component: "SettingsTab",
							requiredPermission: "projects.write",
						},
					],
				},
			},
		});

		const registry = buildRegistryMap([plugin]);
		const regs = registry.get("project.settings.tab");

		expect(regs).toHaveLength(1);
		expect(regs?.[0].requiredPermission).toBe("projects.write");
	});

	it("leaves requiredPermission undefined when the manifest omits it", () => {
		const registry = buildRegistryMap([makePlugin()]);
		const regs = registry.get("project.settings.tab");

		expect(regs).toHaveLength(1);
		expect(regs?.[0].requiredPermission).toBeUndefined();
	});

	it("excludes registrations from disabled plugins", () => {
		const registry = buildRegistryMap([makePlugin({ enabled: false })]);
		expect(registry.get("project.settings.tab")).toBeUndefined();
	});
});
