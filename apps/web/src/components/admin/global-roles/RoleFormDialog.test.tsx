import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/lib/plugin-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/plugin-api")>(
			"@/lib/plugin-api",
		);
	return {
		...actual,
		pluginsQueryOptions: { queryKey: ["plugins"], queryFn: async () => [] },
	};
});

import type { Role } from "@/lib/role-api";
import { RoleFormDialog } from "./RoleFormDialog";

function Wrapper({ children }: { children: ReactNode }) {
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

const role = (resources: string[], actions: string[]): Role => ({
	id: "r1",
	name: "Developer",
	description: "",
	project_id: null,
	policy: {
		version: "2026-10-01",
		statements: [{ sid: "Migrated", effect: "Allow", actions, resources }],
	},
	is_system: false,
	is_default: false,
	attachment_count: 0,
	created_at: "2026-01-01T00:00:00.000Z",
	updated_at: "2026-01-01T00:00:00.000Z",
});

describe("workspace RoleFormDialog", () => {
	it("opens a project-role template converted from the old model in the simple view", () => {
		render(
			<Wrapper>
				<RoleFormDialog
					open
					onOpenChange={() => {}}
					role={role(
						["project/*"],
						[
							"agents:*",
							"annotations:*",
							"conversations:*",
							"docs:*",
							"environments:*",
							"project.activities:read",
							"project.members:read",
							"project.members:write",
							"project.settings.custom_fields:write",
							"project.settings.task_statuses:write",
							"project.settings.task_types:write",
							"project:export",
							"projects:read",
							"projects:write",
							"sprints:*",
							"tasks:*",
							"views:*",
							"workflows:*",
						],
					)}
				/>
			</Wrapper>,
		);

		expect(screen.queryByRole("status")).not.toBeInTheDocument();
		expect(screen.getByRole("switch", { name: "View Tasks" })).toBeChecked();
		expect(screen.getByRole("switch", { name: "View Project" })).toBeChecked();
	});

	it("opens the read-only template, whose roles statement names project/*/role/*", () => {
		const r = role(["project/*"], ["tasks:read", "projects:read", "docs:read"]);
		r.policy.statements.push({
			sid: "MigratedRoles",
			effect: "Allow",
			actions: ["roles:read"],
			resources: ["project/*/role/*"],
		});
		render(
			<Wrapper>
				<RoleFormDialog open onOpenChange={() => {}} role={r} />
			</Wrapper>,
		);

		expect(screen.queryByRole("status")).not.toBeInTheDocument();
		expect(screen.getByRole("switch", { name: "View Tasks" })).toBeChecked();
	});

	it("still opens a workspace role on the workspace permissions", () => {
		render(
			<Wrapper>
				<RoleFormDialog
					open
					onOpenChange={() => {}}
					role={role(
						[
							"user",
							"user/*",
							"role",
							"role/*",
							"plugin",
							"plugin/*",
							"settings",
							"sso",
							"agent",
							"agent/*",
							"project",
						],
						["users:read"],
					)}
				/>
			</Wrapper>,
		);

		expect(screen.queryByRole("status")).not.toBeInTheDocument();
	});
});
