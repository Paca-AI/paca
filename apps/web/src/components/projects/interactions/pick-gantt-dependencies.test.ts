import { describe, expect, it } from "vitest";
import { pickGanttDependencies } from "./view-settings-panel";

describe("pickGanttDependencies", () => {
	it("stores hidden as chosen", () => {
		expect(pickGanttDependencies("hidden", {})).toBe("hidden");
		expect(
			pickGanttDependencies("hidden", { gantt_dependencies: "hidden" }),
		).toBe("hidden");
	});

	it("leaves shown unset when the team default already shows them", () => {
		expect(pickGanttDependencies("shown", {})).toBeUndefined();
		expect(pickGanttDependencies("shown", undefined)).toBeUndefined();
		expect(
			pickGanttDependencies("shown", { gantt_dependencies: "shown" }),
		).toBeUndefined();
	});

	it("stores shown explicitly so it can override a team default that hides them", () => {
		expect(
			pickGanttDependencies("shown", { gantt_dependencies: "hidden" }),
		).toBe("shown");
	});
});
