import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { AutomationNode } from "@/lib/automation-api";
import { AutomationNodeConfigPanel } from "./automation-node-config-panel";

const node = {
	id: "n1",
	automation_id: "a1",
	kind: "condition",
	type: "condition",
	config: {
		branches: [
			{
				handle: "b1",
				label: "",
				tree: { field: "status_id", operator: "equals" },
			},
		],
	},
	pos_x: 0,
	pos_y: 0,
} as unknown as AutomationNode;

function renderPanel() {
	return render(
		<QueryClientProvider client={new QueryClient()}>
			<AutomationNodeConfigPanel
				node={node}
				projectId="p1"
				automationId="a1"
				statuses={[{ id: "s1", name: "Todo" } as never]}
				customFields={[]}
				taskTypes={[
					{ id: "t1", name: "Story" } as never,
					{ id: "t2", name: "Bug" } as never,
				]}
				canEdit
				onSave={vi.fn()}
				onClose={vi.fn()}
				onRemove={vi.fn()}
			/>
		</QueryClientProvider>,
	);
}

describe("condition branch value picker", () => {
	it("lists task types after switching the field from status to task type", async () => {
		renderPanel();
		const [fieldSelect] = screen.getAllByRole("combobox");
		await userEvent.click(fieldSelect);
		const fieldList = await screen.findByRole("listbox");
		await userEvent.click(
			within(fieldList).getByRole("option", { name: /task type/i }),
		);

		const combos = screen.getAllByRole("combobox");
		// field, operator, value
		await userEvent.click(combos[2]);
		const list = await screen.findByRole("listbox");
		expect(within(list).getByRole("option", { name: "Story" })).toBeTruthy();
		expect(within(list).getByRole("option", { name: "Bug" })).toBeTruthy();
	});

	it("clears the old field's value when the field changes", async () => {
		const onSave = vi.fn();
		const withValue = {
			...node,
			config: {
				branches: [
					{
						handle: "b1",
						tree: { field: "status_id", operator: "equals", value: "s1" },
					},
				],
			},
		} as unknown as AutomationNode;
		render(
			<QueryClientProvider client={new QueryClient()}>
				<AutomationNodeConfigPanel
					node={withValue}
					projectId="p1"
					automationId="a1"
					statuses={[{ id: "s1", name: "Todo" } as never]}
					customFields={[]}
					taskTypes={[{ id: "t1", name: "Story" } as never]}
					canEdit
					onSave={onSave}
					onClose={vi.fn()}
					onRemove={vi.fn()}
				/>
			</QueryClientProvider>,
		);
		await userEvent.click(screen.getAllByRole("combobox")[0]);
		await userEvent.click(
			within(await screen.findByRole("listbox")).getByRole("option", {
				name: /task type/i,
			}),
		);
		await userEvent.click(screen.getByRole("button", { name: /save/i }));
		const saved = onSave.mock.calls[0][0] as {
			branches: { tree: { field: string; value?: unknown } }[];
		};
		expect(saved.branches[0].tree.field).toBe("task_type_id");
		expect(saved.branches[0].tree.value).toBeUndefined();
	});
});

function actionNode(type: string, config: Record<string, unknown>) {
	return {
		id: "n2",
		automation_id: "a1",
		kind: "action",
		type,
		config,
		pos_x: 0,
		pos_y: 0,
	} as unknown as AutomationNode;
}

function renderAction(
	type: string,
	config: Record<string, unknown>,
	onSave = vi.fn(),
) {
	render(
		<QueryClientProvider client={new QueryClient()}>
			<AutomationNodeConfigPanel
				node={actionNode(type, config)}
				projectId="p1"
				automationId="a1"
				statuses={[{ id: "s1", name: "Todo" } as never]}
				customFields={[]}
				taskTypes={[{ id: "t1", name: "Story" } as never]}
				canEdit
				onSave={onSave}
				onClose={vi.fn()}
				onRemove={vi.fn()}
			/>
		</QueryClientProvider>,
	);
	return onSave;
}

describe("update_task field operations", () => {
	it("keeps tag add mode, clear and dynamic rows through a save", async () => {
		const onSave = renderAction("update_task", {
			update: {
				tags: ["review"],
				tags_mode: "add",
				clear: ["due_date"],
				dynamic: { assignee_ids: { source: "parent" } },
			},
		});
		await userEvent.click(screen.getByRole("button", { name: /save/i }));
		expect(onSave).toHaveBeenCalledTimes(1);
		const saved = onSave.mock.calls[0][0] as {
			update: Record<string, unknown>;
		};
		expect(saved.update.tags).toEqual(["review"]);
		expect(saved.update.tags_mode).toBe("add");
		expect(saved.update.clear).toEqual(["due_date"]);
		expect(saved.update.dynamic).toEqual({
			assignee_ids: { source: "parent" },
		});
		// The meta keys are not saved as if they were field rows.
		expect(Object.keys(saved.update).sort()).toEqual(
			["clear", "dynamic", "tags", "tags_mode"].sort(),
		);
	});

	it("lets the operation of a row be switched to clear", async () => {
		const onSave = renderAction("update_task", {
			update: { due_date: "2026-01-01T00:00:00Z" },
		});
		// the row's operation dropdown reads "Set to"
		await userEvent.click(screen.getByLabelText(/how to apply/i));
		await userEvent.click(
			within(await screen.findByRole("listbox")).getByRole("option", {
				name: /clear/i,
			}),
		);
		await userEvent.click(screen.getByRole("button", { name: /save/i }));
		const saved = onSave.mock.calls[0][0] as {
			update: Record<string, unknown>;
		};
		expect(saved.update).toEqual({ clear: ["due_date"] });
	});
});

describe("new actions", () => {
	it("add_comment saves its text and needs text to save", async () => {
		const onSave = renderAction("add_comment", {});
		const save = screen.getByRole("button", { name: /save/i });
		expect((save as HTMLButtonElement).disabled).toBe(true);
		await userEvent.type(screen.getAllByRole("textbox")[0], "All done");
		await userEvent.click(save);
		expect(onSave.mock.calls[0][0]).toMatchObject({
			comment: { text: "All done" },
		});
	});

	it("notify needs a recipient and a message", async () => {
		const onSave = renderAction("notify", {});
		const save = screen.getByRole("button", { name: /save/i });
		await userEvent.type(screen.getAllByRole("textbox")[0], "Ready");
		expect((save as HTMLButtonElement).disabled).toBe(true);
		await userEvent.click(screen.getAllByRole("switch")[0]);
		await userEvent.click(save);
		expect(onSave.mock.calls[0][0]).toMatchObject({
			notify: { recipients: ["assignees"], message: "Ready" },
		});
	});

	it("link_task saves a type and a single-task target", async () => {
		const onSave = renderAction("link_task", {
			link: { type: "blocks", to: { kind: "parent" } },
		});
		await userEvent.click(screen.getByRole("button", { name: /save/i }));
		expect(onSave.mock.calls[0][0]).toMatchObject({
			link: { type: "blocks", to: { kind: "parent" } },
		});
	});

	it("create_task keeps its link and dynamic fields", async () => {
		const onSave = renderAction("create_task", {
			create: {
				title: "T",
				link: "blocks",
				dynamic: { due_date: { source: "parent", offset_days: -1 } },
			},
		});
		await userEvent.click(screen.getByRole("button", { name: /save/i }));
		const saved = onSave.mock.calls[0][0] as {
			create: Record<string, unknown>;
		};
		expect(saved.create.title).toBe("T");
		expect(saved.create.link).toBe("blocks");
		expect(saved.create.dynamic).toEqual({
			due_date: { source: "parent", offset_days: -1 },
		});
	});
});
