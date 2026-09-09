import { describe, expect, it } from "vitest";
import {
	ApiErrorCode,
	getApiErrorCode,
	getHttpStatus,
	isForbiddenError,
	isTaskNotFoundError,
} from "./api-error";

describe("getApiErrorCode", () => {
	it("returns known API error code", () => {
		const error = {
			response: {
				data: {
					error_code: ApiErrorCode.InvalidCredentials,
				},
			},
		};

		expect(getApiErrorCode(error)).toBe(ApiErrorCode.InvalidCredentials);
	});

	it("returns null when no error_code exists", () => {
		const error = {
			response: {
				data: {},
			},
		};

		expect(getApiErrorCode(error)).toBeNull();
	});

	it("returns null for unknown error codes", () => {
		const error = {
			response: {
				data: {
					error_code: "SOMETHING_ELSE",
				},
			},
		};

		expect(getApiErrorCode(error)).toBeNull();
	});
});

describe("isTaskNotFoundError", () => {
	it("returns true for a TASK_NOT_FOUND error", () => {
		const error = {
			response: {
				status: 404,
				data: { error_code: ApiErrorCode.TaskNotFound },
			},
		};

		expect(isTaskNotFoundError(error)).toBe(true);
	});

	it("returns false for a transient network/5xx error with no error_code", () => {
		const error = { response: { status: 502, data: {} } };

		expect(isTaskNotFoundError(error)).toBe(false);
	});

	it("returns false for an unrelated error code", () => {
		const error = {
			response: {
				status: 404,
				data: { error_code: ApiErrorCode.ProjectNotFound },
			},
		};

		expect(isTaskNotFoundError(error)).toBe(false);
	});

	it("returns false for a plain network error with no response", () => {
		expect(isTaskNotFoundError(new Error("Network Error"))).toBe(false);
	});
});

describe("getHttpStatus", () => {
	it("returns the status from an axios-shaped error", () => {
		expect(getHttpStatus({ response: { status: 403 } })).toBe(403);
	});

	it("returns undefined for a plain network error with no response", () => {
		expect(getHttpStatus(new Error("Network Error"))).toBeUndefined();
	});
});

describe("isForbiddenError", () => {
	it("returns true for a plain 403", () => {
		const error = {
			response: { status: 403, data: { error_code: "FORBIDDEN" } },
		};
		expect(isForbiddenError(error)).toBe(true);
	});

	it("returns true for a 403 with no error_code at all", () => {
		expect(isForbiddenError({ response: { status: 403, data: {} } })).toBe(
			true,
		);
	});

	it("returns false for AUTH_PASSWORD_CHANGE_REQUIRED — handled by its own redirect, not a generic permission message", () => {
		const error = {
			response: {
				status: 403,
				data: { error_code: ApiErrorCode.PasswordChangeRequired },
			},
		};
		expect(isForbiddenError(error)).toBe(false);
	});

	it("returns false for a 404", () => {
		expect(isForbiddenError({ response: { status: 404, data: {} } })).toBe(
			false,
		);
	});

	it("returns false for a plain network error with no response", () => {
		expect(isForbiddenError(new Error("Network Error"))).toBe(false);
	});
});
