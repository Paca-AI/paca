import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { AssigneePicker } from "@/components/projects/interactions/assignee-picker";
import { MemberSelect } from "./member-select";

const { members } = vi.hoisted(() => ({
	members: [2, 3].map((version) => ({
		id: `member-${version}`,
		project_id: "project-1",
		user_id: `account-${version}`,
		roles: [],
		username: `agent-${version}`,
		full_name: `backend-refactor-agent-v${version}`,
		member_type: "agent",
	})),
}));

vi.mock("@/lib/member-directory", () => ({
	useMemberDirectory: () => (id: string) => members.find((m) => m.id === id),
}));

vi.mock("@/hooks/use-member-search", () => ({
	useMemberSearch: () => ({
		members,
		query: "",
		onQueryChange: vi.fn(),
		hasMore: false,
		isLoadingMore: false,
		onLoadMore: vi.fn(),
	}),
}));

describe("member names in pickers", () => {
	it("preserves distinct endings and selection IDs in the member form dropdown", async () => {
		const user = userEvent.setup();
		const onChange = vi.fn();
		render(
			<MemberSelect
				projectId="project-1"
				value="member-2"
				onChange={onChange}
			/>,
		);

		const trigger = screen.getByRole("button", { name: members[0].full_name });
		expect(within(trigger).getByText("agent-v2")).toBeVisible();
		await user.click(trigger);
		const option = screen.getByRole("button", { name: members[1].full_name });
		expect(within(option).getByTitle(members[1].full_name)).toBeInTheDocument();
		expect(within(option).getByText("agent-v3")).toBeVisible();
		await user.click(option);
		expect(onChange).toHaveBeenCalledWith("member-3");
	});

	it("preserves multi-assignment when long agent names are displayed", async () => {
		const user = userEvent.setup();
		const onChange = vi.fn();
		render(
			<AssigneePicker
				projectId="project-1"
				assigneeIds={["member-2"]}
				canEdit
				showNames
				onChange={onChange}
			/>,
		);

		const trigger = screen.getByRole("button", { name: members[0].full_name });
		expect(within(trigger).getByText("agent-v2")).toBeVisible();
		await user.click(trigger);
		const option = screen.getByRole("button", { name: members[1].full_name });
		expect(within(option).getByText("agent-v3")).toBeVisible();
		await user.click(option);
		expect(onChange).toHaveBeenCalledWith({
			assignee_ids: ["member-2", "member-3"],
		});
	});

	it("also exposes full names in the read-only assignee display", () => {
		render(
			<AssigneePicker
				projectId="project-1"
				assigneeIds={["member-2"]}
				canEdit={false}
				showNames
				onChange={vi.fn()}
			/>,
		);
		expect(screen.getByTitle(members[0].full_name)).toBeInTheDocument();
		expect(screen.getByText("agent-v2")).toBeVisible();
	});
});
