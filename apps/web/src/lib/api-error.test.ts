import { ApiErrorCode, getApiErrorCode } from "./api-error";
import { describe, expect, it } from "vitest";

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
