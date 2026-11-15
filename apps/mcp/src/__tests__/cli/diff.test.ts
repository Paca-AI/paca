import { describe, expect, it } from "vitest";
import { unifiedDiff } from "../../cli/diff.js";

describe("unifiedDiff", () => {
	it("is empty for equal texts", () => {
		expect(unifiedDiff("a\nb\n", "a\nb\n", "x", "y")).toBe("");
	});

	it("shows a changed line with three lines of context", () => {
		const a = "1\n2\n3\n4\n5\n6\n7\n8\n9\n";
		const b = "1\n2\n3\n4\nFIVE\n6\n7\n8\n9\n";
		expect(unifiedDiff(a, b, "last push", "Paca copy")).toBe(
			[
				"--- last push",
				"+++ Paca copy",
				"@@ -2,7 +2,7 @@",
				" 2",
				" 3",
				" 4",
				"-5",
				"+FIVE",
				" 6",
				" 7",
				" 8",
				"",
			].join("\n"),
		);
	});

	it("splits far-apart changes into hunks and counts added lines", () => {
		const a = Array.from({ length: 20 }, (_, i) => `l${i}`).join("\n");
		const b = a.replace("l1", "l1\nnew").replace("l18", "L18");
		const out = unifiedDiff(a, b, "a", "b");
		expect(out).toContain("@@ -1,5 +1,6 @@");
		expect(out).toContain("+new");
		expect(out).toContain("@@ -16,5 +17,5 @@");
		expect(out).toContain("-l18\n+L18");
	});

	it("handles an empty side", () => {
		expect(unifiedDiff("", "x\n", "a", "b")).toBe(
			"--- a\n+++ b\n@@ -0,0 +1,1 @@\n+x\n",
		);
	});
});
