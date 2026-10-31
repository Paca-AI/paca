import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiErrorCode } from "@/lib/api-error";
import { login } from "@/lib/auth-api";
import { loadPendingMfa } from "@/lib/mfa-challenge";
import { useLoginForm } from "./use-login-form";

type SubmitPayload = {
	value: {
		username: string;
		password: string;
		rememberMe: boolean;
	};
};

const mocks = vi.hoisted(() => ({
	navigateMock: vi.fn(),
	invalidateQueriesMock: vi.fn(),
	getApiErrorCodeMock: vi.fn(),
	handleSubmitMock: vi.fn(),
	capturedOnSubmit: null as null | ((args: SubmitPayload) => Promise<void>),
}));

vi.mock("@tanstack/react-router", () => ({
	useNavigate: () => mocks.navigateMock,
}));

vi.mock("@tanstack/react-query", async () => {
	const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
		"@tanstack/react-query",
	);

	return {
		...actual,
		useQueryClient: () => ({
			invalidateQueries: mocks.invalidateQueriesMock,
		}),
	};
});

vi.mock("@tanstack/react-form", () => ({
	useForm: (options: { onSubmit: (args: SubmitPayload) => Promise<void> }) => {
		mocks.capturedOnSubmit = options.onSubmit;
		return { handleSubmit: mocks.handleSubmitMock };
	},
}));

vi.mock("@/lib/auth-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/auth-api")>("@/lib/auth-api");
	return {
		...actual,
		login: vi.fn(),
	};
});

vi.mock("@/lib/api-error", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/api-error")>("@/lib/api-error");
	return {
		...actual,
		getApiErrorCode: mocks.getApiErrorCodeMock,
	};
});

describe("useLoginForm", () => {
	beforeEach(() => {
		mocks.capturedOnSubmit = null;
		mocks.navigateMock.mockReset();
		mocks.invalidateQueriesMock.mockReset();
		mocks.getApiErrorCodeMock.mockReset();
		mocks.handleSubmitMock.mockReset();
		vi.mocked(login).mockReset();
	});

	it("submits login and redirects on success", async () => {
		vi.mocked(login).mockResolvedValue(null);
		mocks.invalidateQueriesMock.mockResolvedValue(undefined);
		mocks.navigateMock.mockResolvedValue(undefined);

		renderHook(() => useLoginForm());

		expect(mocks.capturedOnSubmit).toBeTruthy();
		await act(async () => {
			await mocks.capturedOnSubmit?.({
				value: {
					username: "alice",
					password: "password123",
					rememberMe: true,
				},
			});
		});

		expect(login).toHaveBeenCalledWith("alice", "password123", true);
		// The login flow invalidates the entire "auth" namespace so both the
		// "auth"/"me" and "auth"/"me-optional" caches are refreshed.
		expect(mocks.invalidateQueriesMock).toHaveBeenCalledWith({
			queryKey: ["auth"],
		});
		expect(mocks.navigateMock).toHaveBeenCalledWith({ to: "/home" });
	});

	it("opens the redirect target instead of home when one was given", async () => {
		vi.mocked(login).mockResolvedValue(null);
		mocks.invalidateQueriesMock.mockResolvedValue(undefined);
		const assign = vi.fn();
		const original = window.location;
		Object.defineProperty(window, "location", {
			configurable: true,
			value: { ...original, assign },
		});

		try {
			renderHook(() => useLoginForm("/plugins/com.paca.github/cb?code=1"));
			await act(async () => {
				await mocks.capturedOnSubmit?.({
					value: {
						username: "alice",
						password: "password123",
						rememberMe: false,
					},
				});
			});
		} finally {
			Object.defineProperty(window, "location", {
				configurable: true,
				value: original,
			});
		}

		expect(assign).toHaveBeenCalledWith("/plugins/com.paca.github/cb?code=1");
		expect(mocks.navigateMock).not.toHaveBeenCalled();
	});

	it("sets user-friendly server error for known API error codes", async () => {
		vi.mocked(login).mockRejectedValue(new Error("nope"));
		mocks.getApiErrorCodeMock.mockReturnValue(ApiErrorCode.InvalidCredentials);

		const { result } = renderHook(() => useLoginForm());

		await act(async () => {
			await mocks.capturedOnSubmit?.({
				value: {
					username: "alice",
					password: "wrong-pass",
					rememberMe: false,
				},
			});
		});

		expect(result.current.serverError).toBe("Invalid username or password.");
	});

	it("sets generic error message for unknown API errors", async () => {
		vi.mocked(login).mockRejectedValue(new Error("boom"));
		mocks.getApiErrorCodeMock.mockReturnValue(null);

		const { result } = renderHook(() => useLoginForm());

		await act(async () => {
			await mocks.capturedOnSubmit?.({
				value: {
					username: "alice",
					password: "wrong-pass",
					rememberMe: false,
				},
			});
		});

		expect(result.current.serverError).toBe(
			"Something went wrong. Please try again.",
		);
	});

	it("moves to the code page, keeping the challenge, when a second factor is owed", async () => {
		vi.mocked(login).mockResolvedValue("challenge-token");
		mocks.navigateMock.mockResolvedValue(undefined);

		const { result } = renderHook(() => useLoginForm("/projects/1"));
		await act(async () => {
			await mocks.capturedOnSubmit?.({
				value: {
					username: "alice",
					password: "password123",
					rememberMe: false,
				},
			});
		});

		expect(mocks.navigateMock).toHaveBeenCalledWith({ to: "/login/mfa" });
		expect(loadPendingMfa()).toEqual({
			challenge: "challenge-token",
			redirectTo: "/projects/1",
		});
		// No session yet, so nothing signed in and no error either.
		expect(mocks.invalidateQueriesMock).not.toHaveBeenCalled();
		expect(result.current.serverError).toBeNull();
	});

	it("offers in-place setup when policy requires MFA the account lacks", async () => {
		vi.mocked(login).mockRejectedValue(new Error("setup"));
		mocks.getApiErrorCodeMock.mockReturnValue(ApiErrorCode.MfaSetupRequired);

		const { result } = renderHook(() => useLoginForm());
		await act(async () => {
			await mocks.capturedOnSubmit?.({
				value: { username: "alice", password: "password123", rememberMe: true },
			});
		});

		expect(result.current.mfaSetup).toEqual({
			username: "alice",
			password: "password123",
			rememberMe: true,
		});
		expect(result.current.serverError).toBeNull();
	});
});
