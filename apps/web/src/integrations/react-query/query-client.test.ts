import { describe, expect, it } from "vitest";
import { shouldRetryQuery } from "./query-client";

function axiosErrorWithStatus(status: number): unknown {
	return { response: { status } };
}

describe("shouldRetryQuery", () => {
	it("does not retry a 403 (permission denied)", () => {
		expect(shouldRetryQuery(0, axiosErrorWithStatus(403))).toBe(false);
	});

	it("does not retry a 401 (unauthenticated)", () => {
		expect(shouldRetryQuery(0, axiosErrorWithStatus(401))).toBe(false);
	});

	it("does not retry a 404 (not found)", () => {
		expect(shouldRetryQuery(0, axiosErrorWithStatus(404))).toBe(false);
	});

	it("does not retry a 400 (bad request)", () => {
		expect(shouldRetryQuery(0, axiosErrorWithStatus(400))).toBe(false);
	});

	it("retries a 500 up to 3 times", () => {
		expect(shouldRetryQuery(0, axiosErrorWithStatus(500))).toBe(true);
		expect(shouldRetryQuery(2, axiosErrorWithStatus(500))).toBe(true);
		expect(shouldRetryQuery(3, axiosErrorWithStatus(500))).toBe(false);
	});

	it("retries a network error with no response (e.g. connection refused)", () => {
		expect(shouldRetryQuery(0, new Error("Network Error"))).toBe(true);
		expect(shouldRetryQuery(3, new Error("Network Error"))).toBe(false);
	});
});
