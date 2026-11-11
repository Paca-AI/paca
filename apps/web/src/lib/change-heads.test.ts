import { beforeEach, describe, expect, it } from "vitest";
import {
	clearChangeHeads,
	earliestChangeHead,
	markJoined,
	markLeft,
	observeChangeHead,
	projectIdOfUrl,
} from "./change-heads";

const P = "9c18da4b-18b9-4d66-8d82-7a65202b91a4";

describe("change heads", () => {
	beforeEach(clearChangeHeads);

	it("keeps the lowest head seen before the project is joined, whatever order responses arrive in", () => {
		observeChangeHead("p", 5, 100);
		observeChangeHead("p", 9, 100);
		expect(earliestChangeHead("p")).toEqual({ seq: 5, epoch: 100 });
		clearChangeHeads();
		observeChangeHead("p", 9, 100);
		observeChangeHead("p", 5, 100);
		expect(earliestChangeHead("p")).toEqual({ seq: 5, epoch: 100 });
	});

	it("takes the newer log when the epoch changed", () => {
		observeChangeHead("p", 50, 100);
		observeChangeHead("p", 2, 200);
		expect(earliestChangeHead("p")).toEqual({ seq: 2, epoch: 200 });
	});

	it("stops collecting once joined, and starts again after leaving", () => {
		observeChangeHead("p", 5, 100);
		markJoined("p");
		expect(earliestChangeHead("p")).toBeUndefined();
		observeChangeHead("p", 8, 100);
		expect(earliestChangeHead("p")).toBeUndefined();
		markLeft("p");
		observeChangeHead("p", 12, 100);
		expect(earliestChangeHead("p")).toEqual({ seq: 12, epoch: 100 });
	});

	it("ignores a head that is not a number, and keeps projects apart", () => {
		observeChangeHead("p", Number.NaN, 1);
		expect(earliestChangeHead("p")).toBeUndefined();
		observeChangeHead("a", 1, 1);
		observeChangeHead("b", 2, 1);
		expect(earliestChangeHead("a")?.seq).toBe(1);
		expect(earliestChangeHead("b")?.seq).toBe(2);
	});

	it("finds the project of a request url", () => {
		expect(projectIdOfUrl(`/projects/${P}/tasks?x=1`)).toBe(P);
		expect(projectIdOfUrl(`/projects/${P}`)).toBe(P);
		expect(projectIdOfUrl(`http://h/api/v1/projects/${P}/docs`)).toBe(P);
		expect(projectIdOfUrl("/projects/all")).toBeNull();
		expect(projectIdOfUrl("/users/me/tasks")).toBeNull();
		expect(projectIdOfUrl(undefined)).toBeNull();
	});
});
