// Tests for the banner shown above a document that is a managed copy of a
// repository file.

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ManagedCopyBanner } from "./managed-copy-banner";

const source = {
	repository: "github.com/acme/app",
	path: "docs/plan.md",
	commit: "0123456789abcdef",
};

describe("ManagedCopyBanner", () => {
	it("renders nothing for a document without a source", () => {
		const { container } = render(
			<ManagedCopyBanner
				source={undefined}
				locked={false}
				canWrite
				onToggleLock={vi.fn()}
			/>,
		);
		expect(container).toBeEmptyDOMElement();
	});

	it("names the file, the repository and the short commit", () => {
		render(
			<ManagedCopyBanner
				source={source}
				locked
				canWrite
				onToggleLock={vi.fn()}
			/>,
		);
		expect(
			screen.getByText(
				"Copy of docs/plan.md in github.com/acme/app at 0123456",
			),
		).toBeInTheDocument();
		expect(
			screen.getByText("Locked: edit the file in the repository and push it."),
		).toBeInTheDocument();
	});

	it("leaves the commit out when the source has none", () => {
		render(
			<ManagedCopyBanner
				source={{ repository: "github.com/acme/app", path: "docs/plan.md" }}
				locked={false}
				canWrite
				onToggleLock={vi.fn()}
			/>,
		);
		expect(
			screen.getByText("Copy of docs/plan.md in github.com/acme/app"),
		).toBeInTheDocument();
		expect(
			screen.getByText(
				"Edits made here are overwritten by the next push unless they are merged into the file.",
			),
		).toBeInTheDocument();
	});

	it("shows no lock button without write permission", () => {
		render(
			<ManagedCopyBanner
				source={source}
				locked
				canWrite={false}
				onToggleLock={vi.fn()}
			/>,
		);
		expect(screen.queryByRole("button")).not.toBeInTheDocument();
		// The explanation is still there for readers.
		expect(
			screen.getByText("Locked: edit the file in the repository and push it."),
		).toBeInTheDocument();
	});

	it("asks for the opposite lock state and follows the locked prop", async () => {
		const onToggleLock = vi.fn();
		const { rerender } = render(
			<ManagedCopyBanner
				source={source}
				locked
				canWrite
				onToggleLock={onToggleLock}
			/>,
		);
		await userEvent.click(screen.getByRole("button", { name: "Unlock" }));
		expect(onToggleLock).toHaveBeenCalledWith(false);

		// The page passes the state from the server's answer.
		rerender(
			<ManagedCopyBanner
				source={source}
				locked={false}
				canWrite
				onToggleLock={onToggleLock}
			/>,
		);
		await userEvent.click(screen.getByRole("button", { name: "Lock" }));
		expect(onToggleLock).toHaveBeenLastCalledWith(true);
	});

	it("keeps the label when the call fails (locked prop unchanged)", async () => {
		const onToggleLock = vi.fn();
		const { rerender } = render(
			<ManagedCopyBanner
				source={source}
				locked
				canWrite
				onToggleLock={onToggleLock}
			/>,
		);
		await userEvent.click(screen.getByRole("button", { name: "Unlock" }));
		rerender(
			<ManagedCopyBanner
				source={source}
				locked
				canWrite
				onToggleLock={onToggleLock}
				pending={false}
			/>,
		);
		expect(screen.getByRole("button", { name: "Unlock" })).toBeEnabled();
	});

	it("disables the button while the change is pending", () => {
		render(
			<ManagedCopyBanner
				source={source}
				locked
				canWrite
				onToggleLock={vi.fn()}
				pending
			/>,
		);
		expect(screen.getByRole("button", { name: "Unlock" })).toBeDisabled();
	});
});
