import { describe, expect, it } from "vitest";
import { pickCalendarDateField } from "./view-settings-panel";

describe("pickCalendarDateField", () => {
	it("stores a single date field as chosen", () => {
		expect(pickCalendarDateField("due_date", {})).toBe("due_date");
		expect(
			pickCalendarDateField("start_date", { calendar_date_field: "due_date" }),
		).toBe("start_date");
	});

	it("leaves start and due unset when the team default already uses both", () => {
		expect(pickCalendarDateField("start_and_due", {})).toBeUndefined();
		expect(pickCalendarDateField("start_and_due", undefined)).toBeUndefined();
		expect(
			pickCalendarDateField("start_and_due", {
				calendar_date_field: "start_and_due",
			}),
		).toBeUndefined();
	});

	it("stores start and due explicitly so it can override a team default of one date", () => {
		expect(
			pickCalendarDateField("start_and_due", {
				calendar_date_field: "due_date",
			}),
		).toBe("start_and_due");
	});
});
