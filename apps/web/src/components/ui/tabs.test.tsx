import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "./tabs";

describe("Tabs", () => {
	it("supports roving focus and labelled panels", async () => {
		const user = userEvent.setup();
		render(
			<Tabs defaultValue="task">
				<TabsList>
					<TabsTrigger value="task">Tasks</TabsTrigger>
					<TabsTrigger value="session">Chats</TabsTrigger>
					<TabsTrigger value="run">Runs</TabsTrigger>
				</TabsList>
				<TabsContent value="task">Task panel</TabsContent>
				<TabsContent value="session">Chat panel</TabsContent>
				<TabsContent value="run">Run panel</TabsContent>
			</Tabs>,
		);

		const tasks = screen.getByRole("tab", { name: "Tasks" });
		const chats = screen.getByRole("tab", { name: "Chats" });
		expect(tasks).toHaveAttribute("tabindex", "0");
		expect(chats).toHaveAttribute("tabindex", "-1");
		tasks.focus();
		await user.keyboard("{ArrowRight}");

		expect(chats).toHaveFocus();
		expect(chats).toHaveAttribute("aria-selected", "true");
		const panel = screen.getByRole("tabpanel");
		expect(panel).toHaveTextContent("Chat panel");
		expect(panel).toHaveAttribute("aria-labelledby", chats.id);
		expect(chats).toHaveAttribute("aria-controls", panel.id);

		await user.keyboard("{End}");
		expect(screen.getByRole("tab", { name: "Runs" })).toHaveFocus();
		await user.keyboard("{Home}");
		expect(tasks).toHaveFocus();
	});
});
