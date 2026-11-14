// Tests for the change note shown under a doc activity entry.

import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { DocActivityNoteLine, getDocActivityNote } from "./doc-activity-pane";

describe("getDocActivityNote", () => {
	it("reads the note, client and task a write recorded", () => {
		expect(
			getDocActivityNote({
				activity_type: "doc.updated",
				content: {
					changes: [{ field: "content", old: [], new: [] }],
					note: "rewrote step 2",
					client: "claude-code/s1",
					task: "PACA-88",
				},
			}),
		).toEqual({
			note: "rewrote step 2",
			client: "claude-code/s1",
			task: "PACA-88",
		});
		expect(
			getDocActivityNote({
				activity_type: "doc.created",
				content: { title: "Doc", note: "first draft" },
			}),
		).toEqual({ note: "first draft" });
	});

	it("returns null for entries without one and for other entry types", () => {
		expect(
			getDocActivityNote({
				activity_type: "doc.updated",
				content: { changes: [{ field: "title", old: "a", new: "b" }] },
			}),
		).toBeNull();
		expect(
			getDocActivityNote({ activity_type: "comment", content: "hi" }),
		).toBeNull();
		expect(
			getDocActivityNote({
				activity_type: "doc.moved",
				content: { note: "not a write note" },
			}),
		).toBeNull();
	});
});

describe("DocActivityNoteLine", () => {
	it("shows the note as plain text with the task and client below", () => {
		render(
			<DocActivityNoteLine
				note={{ note: "<b>why</b>", task: "PACA-88", client: "claude-code/s1" }}
			/>,
		);
		expect(screen.getByText("<b>why</b>")).toBeInTheDocument();
		expect(screen.getByText("PACA-88 · claude-code/s1")).toBeInTheDocument();
	});
});
