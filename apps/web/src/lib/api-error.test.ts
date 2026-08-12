import { describe, expect, it } from "vitest";
import {
	ApiErrorCode,
	getApiErrorCode,
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
