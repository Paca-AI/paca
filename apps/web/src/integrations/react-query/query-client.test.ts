import type { Mutation } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "@/components/ui/toast";
import {
	handleMutationError,
	handleMutationSuccess,
	shouldRetryQuery,
} from "./query-client";

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

describe("handleMutationError", () => {
	const mutationWith = (options: object = {}, meta?: Record<string, unknown>) =>
		({ options, meta }) as unknown as Mutation<
			unknown,
			unknown,
			unknown,
			unknown
		>;

	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it("toasts the server message when the mutation has no onError", () => {
		const spy = vi.spyOn(toast, "add").mockReturnValue("id");
		handleMutationError(
			{ response: { status: 400, data: { error: "Name is taken" } } },
			mutationWith(),
		);
		expect(spy).toHaveBeenCalledWith(
			expect.objectContaining({ type: "error", description: "Name is taken" }),
		);
	});

	it("stays quiet when the mutation handles its own error", () => {
		const spy = vi.spyOn(toast, "add").mockReturnValue("id");
		handleMutationError(new Error("x"), mutationWith({ onError: () => {} }));
		expect(spy).not.toHaveBeenCalled();
	});

	it("stays quiet when the mutation is marked silent", () => {
		const spy = vi.spyOn(toast, "add").mockReturnValue("id");
		handleMutationError(new Error("x"), mutationWith({}, { silent: true }));
		expect(spy).not.toHaveBeenCalled();
	});

	it("stays quiet on 401", () => {
		const spy = vi.spyOn(toast, "add").mockReturnValue("id");
		handleMutationError({ response: { status: 401 } }, mutationWith());
		expect(spy).not.toHaveBeenCalled();
	});
});

describe("handleMutationSuccess", () => {
	const withMeta = (meta?: Record<string, unknown>) =>
		({ meta }) as unknown as Mutation<unknown, unknown, unknown, unknown>;

	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it("toasts when the mutation opts in", () => {
		const spy = vi.spyOn(toast, "add").mockReturnValue("id");
		handleMutationSuccess(withMeta({ success: "saved" }));
		expect(spy).toHaveBeenCalledTimes(1);
	});

	it("stays quiet without the opt-in", () => {
		const spy = vi.spyOn(toast, "add").mockReturnValue("id");
		handleMutationSuccess(withMeta());
		expect(spy).not.toHaveBeenCalled();
	});
});
