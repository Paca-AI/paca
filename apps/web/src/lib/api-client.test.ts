import axios from "axios";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("axios", () => ({
	default: {
		create: vi.fn(() => ({
			interceptors: {
				request: { use: vi.fn() },
				response: { use: vi.fn() },
			},
			request: vi.fn(),
			post: vi.fn(),
		})),
	},
}));

import { ApiClient } from "./api-client";

type ResponseErrorHandler = (error: {
	config: { url?: string; _retry?: boolean };
	response?: { status?: number };
}) => Promise<unknown>;

type MockAxiosInstance = {
	interceptors: {
		request: {
			use: ReturnType<typeof vi.fn>;
		};
		response: {
			use: ReturnType<typeof vi.fn>;
		};
	};
	request: ReturnType<typeof vi.fn>;
	post: ReturnType<typeof vi.fn>;
};

function createAxiosInstanceMock(): MockAxiosInstance {
	return {
		interceptors: {
			request: {
				use: vi.fn(),
			},
			response: {
				use: vi.fn(),
			},
		},
		request: vi.fn(),
		post: vi.fn(),
	};
}

function getResponseErrorHandler(mockInstance: MockAxiosInstance) {
	return mockInstance.interceptors.response.use.mock
		.calls[0][1] as ResponseErrorHandler;
}

describe("ApiClient", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("creates axios instance with API defaults", () => {
		const mockInstance = createAxiosInstanceMock();
		vi.mocked(axios.create).mockReturnValue(mockInstance as never);

		new ApiClient();

		expect(axios.create).toHaveBeenCalledWith({
			baseURL: "http://localhost:8080/api/v1",
			withCredentials: true,
			headers: {
				"Content-Type": "application/json",
			},
		});
		expect(mockInstance.interceptors.request.use).toHaveBeenCalledTimes(1);
		expect(mockInstance.interceptors.response.use).toHaveBeenCalledTimes(1);
	});

	it("rejects non-401 responses without refresh", async () => {
		const mockInstance = createAxiosInstanceMock();
		vi.mocked(axios.create).mockReturnValue(mockInstance as never);

		new ApiClient();
		const onResponseError = getResponseErrorHandler(mockInstance);
		const error = {
			config: { url: "/users/me" },
			response: { status: 500 },
		};

		await expect(onResponseError(error)).rejects.toBe(error);
		expect(mockInstance.post).not.toHaveBeenCalled();
		expect(mockInstance.request).not.toHaveBeenCalled();
	});

	it("rejects auth endpoint 401 errors without refresh", async () => {
		const mockInstance = createAxiosInstanceMock();
		vi.mocked(axios.create).mockReturnValue(mockInstance as never);

		new ApiClient();
		const onResponseError = getResponseErrorHandler(mockInstance);
		const error = {
			config: { url: "/auth/login" },
			response: { status: 401 },
		};

		await expect(onResponseError(error)).rejects.toBe(error);
		expect(mockInstance.post).not.toHaveBeenCalled();
		expect(mockInstance.request).not.toHaveBeenCalled();
	});

	it("refreshes and retries a 401 request once", async () => {
		const mockInstance = createAxiosInstanceMock();
		vi.mocked(axios.create).mockReturnValue(mockInstance as never);
		mockInstance.post.mockResolvedValue({});
		mockInstance.request.mockResolvedValue({ data: { ok: true } });

		new ApiClient();
		const onResponseError = getResponseErrorHandler(mockInstance);
		const originalRequest: { url: string; _retry?: boolean } = {
			url: "/users/me",
		};

		await expect(
			onResponseError({ config: originalRequest, response: { status: 401 } }),
		).resolves.toEqual({ data: { ok: true } });

		expect(mockInstance.post).toHaveBeenCalledWith("/auth/refresh");
		expect(originalRequest._retry).toBe(true);
		expect(mockInstance.request).toHaveBeenCalledWith(originalRequest);
	});

	it("queues concurrent 401 requests while refresh is in flight", async () => {
		const mockInstance = createAxiosInstanceMock();
		vi.mocked(axios.create).mockReturnValue(mockInstance as never);

		let resolveRefresh: (() => void) | undefined;
		const refreshPromise = new Promise<void>((resolve) => {
			resolveRefresh = resolve;
		});

		mockInstance.post.mockReturnValue(refreshPromise);
		mockInstance.request.mockImplementation((config: { url?: string }) => {
			if (config.url === "/users/me") {
				return Promise.resolve({ data: { first: true } });
			}

			if (config.url === "/projects") {
				return Promise.resolve({ data: { queued: true } });
			}

			return Promise.resolve({ data: { ok: true } });
		});

		new ApiClient();
		const onResponseError = getResponseErrorHandler(mockInstance);

		const first = onResponseError({
			config: { url: "/users/me" },
			response: { status: 401 },
		});

		const queued = onResponseError({
			config: { url: "/projects" },
			response: { status: 401 },
		});

		resolveRefresh?.();

		await expect(first).resolves.toEqual({ data: { first: true } });
		await expect(queued).resolves.toEqual({ data: { queued: true } });

		expect(mockInstance.post).toHaveBeenCalledTimes(1);
		expect(mockInstance.request).toHaveBeenCalledTimes(2);
	});

	it("rejects with refresh error and allows future retries", async () => {
		const mockInstance = createAxiosInstanceMock();
		vi.mocked(axios.create).mockReturnValue(mockInstance as never);
		const refreshError = new Error("refresh failed");
		mockInstance.post
			.mockRejectedValueOnce(refreshError)
			.mockResolvedValueOnce({});
		mockInstance.request.mockResolvedValue({ data: { ok: true } });

		new ApiClient();
		const onResponseError = getResponseErrorHandler(mockInstance);

		await expect(
			onResponseError({
				config: { url: "/users/me" },
				response: { status: 401 },
			}),
		).rejects.toBe(refreshError);

		await expect(
			onResponseError({
				config: { url: "/users/me" },
				response: { status: 401 },
			}),
		).resolves.toEqual({ data: { ok: true } });

		expect(mockInstance.post).toHaveBeenCalledTimes(2);
	});

	it("does not retry if request is already marked as retried", async () => {
		const mockInstance = createAxiosInstanceMock();
		vi.mocked(axios.create).mockReturnValue(mockInstance as never);

		new ApiClient();
		const onResponseError = getResponseErrorHandler(mockInstance);
		const error = {
			config: { url: "/users/me", _retry: true },
			response: { status: 401 },
		};

		await expect(onResponseError(error)).rejects.toBe(error);
		expect(mockInstance.post).not.toHaveBeenCalled();
		expect(mockInstance.request).not.toHaveBeenCalled();
	});
});
