import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockPatch, mockPost } = vi.hoisted(() => ({
	mockPatch: vi.fn(),
	mockPost: vi.fn(),
}));

vi.mock("./api-client", () => ({
	apiClient: { instance: { patch: mockPatch, post: mockPost } },
}));

import { testProjectJevConfig, updateProjectJevConfig } from "./project-api";

beforeEach(() => {
	mockPatch.mockReset();
	mockPost.mockReset();
});

describe("updateProjectJevConfig", () => {
	it("PATCHes the jev-config endpoint and unwraps the envelope", async () => {
		const config = { configured: true, base_url: "https://h", model: "m" };
		mockPatch.mockResolvedValue({ data: { success: true, data: config } });

		const result = await updateProjectJevConfig("p1", {
			api_key: "k",
			model: "m",
		});

		expect(mockPatch).toHaveBeenCalledWith("/projects/p1/jev-config", {
			api_key: "k",
			model: "m",
		});
		expect(result).toEqual(config);
	});

	it("propagates request failures", async () => {
		mockPatch.mockRejectedValue(new Error("403"));
		await expect(updateProjectJevConfig("p1", {})).rejects.toThrow("403");
	});
});

describe("testProjectJevConfig", () => {
	it("POSTs to the test endpoint and unwraps the envelope", async () => {
		mockPost.mockResolvedValue({
			data: { success: true, data: { success: true } },
		});
		await expect(testProjectJevConfig("p1")).resolves.toEqual({
			success: true,
		});
		expect(mockPost).toHaveBeenCalledWith("/projects/p1/jev-config/test");
	});

	it("rejects when the connection test fails", async () => {
		mockPost.mockRejectedValue(new Error("Could not reach Jev"));
		await expect(testProjectJevConfig("p1")).rejects.toThrow(
			"Could not reach Jev",
		);
	});
});
