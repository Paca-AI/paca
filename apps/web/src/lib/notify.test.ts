import { beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "@/components/ui/toast";

const feedback = vi.hoisted(() => ({ enabled: true, openIssue: vi.fn() }));
vi.mock("@/lib/settings-api", () => ({
	isFeedbackEnabledCached: () => feedback.enabled,
}));
vi.mock("@/lib/feedback", () => ({
	draftFromError: (e: unknown) => ({ kind: "bug", title: String(e) }),
	openIssue: feedback.openIssue,
}));

import { notifyError, showErrorToast } from "./notify";

function lastToast() {
	const spy = vi.mocked(toast.add);
	return spy.mock.calls[spy.mock.calls.length - 1]?.[0] as {
		type: string;
		title: string;
		description?: string;
		actionProps?: { children: string; onClick: () => void };
	};
}

describe("error toasts", () => {
	beforeEach(() => {
		vi.restoreAllMocks();
		vi.spyOn(toast, "add").mockReturnValue("id");
		feedback.enabled = true;
		feedback.openIssue.mockClear();
	});

	it("offers a report button that opens a pre-filled issue", () => {
		const err = new Error("boom");
		notifyError(err, "Could not save");
		const t = lastToast();
		expect(t.type).toBe("error");
		expect(t.title).toBe("Could not save");
		expect(t.actionProps?.children).toBe("Report this issue");
		t.actionProps?.onClick();
		expect(feedback.openIssue).toHaveBeenCalledWith({
			kind: "bug",
			title: "Error: boom",
		});
	});

	it("has no report button when feedback is turned off", () => {
		feedback.enabled = false;
		notifyError(new Error("boom"));
		expect(lastToast().actionProps).toBeUndefined();
	});

	it("has no report button for a permission (403) error", () => {
		notifyError({ response: { status: 403 } });
		expect(lastToast().actionProps).toBeUndefined();
	});

	it("has no report button when there is no underlying error", () => {
		showErrorToast({ title: "Couldn't copy to the clipboard" });
		expect(lastToast().actionProps).toBeUndefined();
	});

	it("uses the server message as the description, or none when suppressed", () => {
		const err = { response: { status: 400, data: { error: "Name is taken" } } };
		notifyError(err);
		expect(lastToast().description).toBe("Name is taken");
		showErrorToast({ title: "Name is taken", description: "", error: err });
		expect(lastToast().description).toBeUndefined();
	});
});
