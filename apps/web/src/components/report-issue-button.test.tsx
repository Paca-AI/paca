import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ReportIssueButton } from "./report-issue-button";

describe("ReportIssueButton", () => {
	it("opens a pre-filled bug report with the error details", () => {
		const open = vi.spyOn(window, "open").mockImplementation(() => null);
		render(<ReportIssueButton error={new Error("kaput")} />);

		screen.getByRole("button", { name: /report this issue/i }).click();

		const url = new URL(String(open.mock.calls[0][0]));
		expect(url.searchParams.get("template")).toBe("bug_report.yml");
		expect(url.searchParams.get("logs")).toContain("kaput");
		open.mockRestore();
	});

	it("renders nothing for a permission error", () => {
		const error = Object.assign(new Error("nope"), {
			response: { status: 403, data: { error_code: "FORBIDDEN" } },
		});
		const { container } = render(<ReportIssueButton error={error} />);
		expect(container).toBeEmptyDOMElement();
	});
});
