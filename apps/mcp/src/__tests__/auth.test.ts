import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Mock markdownToBlocknote so the client import chain doesn't need a live
// BlockNote editor (same approach as client.test.ts).
vi.mock("../utils/index.js", () => ({
	markdownToBlocknote: vi.fn((md: string) => [{ type: "paragraph", text: md }]),
	blocknoteToMarkdown: vi.fn(() => ""),
}));

import { PacaAPIClient } from "../api/client.js";
import {
	AuthTokenRejectedError,
	buildAuthHeaders,
	registerAuthResponse,
	resetAuthRejection,
	resolveAuthEnv,
} from "../auth.js";
import type { PacaConfig } from "../types/index.js";

const apikeyConfig: PacaConfig = {
	apiKey: "test-key",
	baseURL: "http://api.test",
	agentId: "agent-1",
	projectId: "project-1",
};

const bearerConfig: PacaConfig = {
	apiKey: "",
	baseURL: "http://api.test",
	projectId: "project-1",
	authMode: "bearer",
	bearerToken: "rs256.platform.token",
};

beforeEach(() => {
	resetAuthRejection();
});

afterEach(() => {
	resetAuthRejection();
	vi.unstubAllGlobals();
});

// ---------------------------------------------------------------------------
// resolveAuthEnv
// ---------------------------------------------------------------------------

describe("resolveAuthEnv", () => {
	it("defaults to apikey mode when PACA_AUTH_MODE is unset", () => {
		expect(resolveAuthEnv({})).toEqual({ authMode: "apikey" });
	});

	it("accepts explicit apikey mode", () => {
		expect(resolveAuthEnv({ PACA_AUTH_MODE: "apikey" })).toEqual({
			authMode: "apikey",
		});
	});

	it("is case-insensitive and trims the mode value", () => {
		expect(
			resolveAuthEnv({ PACA_AUTH_MODE: " Bearer ", PACA_MCP_TOKEN: "tok" }),
		).toEqual({ authMode: "bearer", bearerToken: "tok" });
	});

	it("rejects unknown modes", () => {
		expect(() => resolveAuthEnv({ PACA_AUTH_MODE: "magic" })).toThrow(
			/PACA_AUTH_MODE must be "apikey" or "bearer"/,
		);
	});

	it("fails fast when bearer mode has no PACA_MCP_TOKEN", () => {
		expect(() => resolveAuthEnv({ PACA_AUTH_MODE: "bearer" })).toThrow(
			/requires PACA_MCP_TOKEN/,
		);
	});

	it("fails fast when bearer mode has a blank PACA_MCP_TOKEN", () => {
		expect(() =>
			resolveAuthEnv({ PACA_AUTH_MODE: "bearer", PACA_MCP_TOKEN: "   " }),
		).toThrow(/requires PACA_MCP_TOKEN/);
	});
});

// ---------------------------------------------------------------------------
// buildAuthHeaders
// ---------------------------------------------------------------------------

describe("buildAuthHeaders", () => {
	it("apikey mode sends X-API-Key and X-Agent-ID (upstream behavior)", () => {
		expect(buildAuthHeaders(apikeyConfig)).toEqual({
			"X-API-Key": "test-key",
			"X-Agent-ID": "agent-1",
		});
	});

	it("apikey mode omits X-Agent-ID when no agent id is configured", () => {
		expect(buildAuthHeaders({ ...apikeyConfig, agentId: undefined })).toEqual({
			"X-API-Key": "test-key",
		});
	});

	it("bearer mode sends ONLY the Authorization header — never X-API-Key or X-Agent-ID", () => {
		const headers = buildAuthHeaders(bearerConfig);
		expect(headers).toEqual({ Authorization: "Bearer rs256.platform.token" });
		expect(headers["X-API-Key"]).toBeUndefined();
		expect(headers["X-Agent-ID"]).toBeUndefined();
	});

	it("bearer mode never sends X-Agent-ID even if an agentId sneaks into config", () => {
		// index.ts drops agentId in bearer mode; defense in depth here.
		expect(buildAuthHeaders({ ...bearerConfig, agentId: "agent-1" })).toEqual({
			Authorization: "Bearer rs256.platform.token",
		});
	});

	it("bearer mode without a token throws instead of sending nothing", () => {
		expect(() =>
			buildAuthHeaders({ ...bearerConfig, bearerToken: undefined }),
		).toThrow(AuthTokenRejectedError);
	});
});

// ---------------------------------------------------------------------------
// registerAuthResponse — the 401 latch (expired/invalid token behavior)
// ---------------------------------------------------------------------------

describe("registerAuthResponse", () => {
	it("latches on a bearer 401: throws a clear do-not-retry error", () => {
		expect(() => registerAuthResponse(401, bearerConfig)).toThrow(
			/do NOT retry/,
		);
	});

	it("after the latch, buildAuthHeaders fails immediately (no further requests)", () => {
		expect(() => registerAuthResponse(401, bearerConfig)).toThrow(
			AuthTokenRejectedError,
		);
		expect(() => buildAuthHeaders(bearerConfig)).toThrow(
			AuthTokenRejectedError,
		);
	});

	it("does nothing on non-401 statuses in bearer mode", () => {
		expect(() => registerAuthResponse(403, bearerConfig)).not.toThrow();
		expect(() => registerAuthResponse(500, bearerConfig)).not.toThrow();
		expect(() => buildAuthHeaders(bearerConfig)).not.toThrow();
	});

	it("does nothing on 401 in apikey mode (upstream error handling applies)", () => {
		expect(() => registerAuthResponse(401, apikeyConfig)).not.toThrow();
		expect(() => buildAuthHeaders(apikeyConfig)).not.toThrow();
	});
});

// ---------------------------------------------------------------------------
// PacaAPIClient integration — both modes + expired-token flow
// ---------------------------------------------------------------------------

describe("PacaAPIClient auth integration", () => {
	it("apikey mode requests carry X-API-Key + X-Agent-ID", async () => {
		const fetchMock = vi.fn().mockResolvedValueOnce({
			ok: true,
			status: 200,
			json: async () => ({ success: true, data: { items: [] } }),
		} as unknown as Response);
		vi.stubGlobal("fetch", fetchMock);

		await new PacaAPIClient(apikeyConfig).listProjects();

		const headers = fetchMock.mock.calls[0][1].headers as Record<
			string,
			string
		>;
		expect(headers["X-API-Key"]).toBe("test-key");
		expect(headers["X-Agent-ID"]).toBe("agent-1");
		expect(headers.Authorization).toBeUndefined();
	});

	it("bearer mode requests carry Authorization and no identity headers", async () => {
		const fetchMock = vi.fn().mockResolvedValueOnce({
			ok: true,
			status: 200,
			json: async () => ({ success: true, data: { items: [] } }),
		} as unknown as Response);
		vi.stubGlobal("fetch", fetchMock);

		await new PacaAPIClient(bearerConfig).listProjects();

		const headers = fetchMock.mock.calls[0][1].headers as Record<
			string,
			string
		>;
		expect(headers.Authorization).toBe("Bearer rs256.platform.token");
		expect(headers["X-API-Key"]).toBeUndefined();
		expect(headers["X-Agent-ID"]).toBeUndefined();
	});

	it("expired bearer token: first 401 throws a clear error, second call never hits the API", async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: false,
			status: 401,
			statusText: "Unauthorized",
			text: async () => '{"error":"platform bearer token rejected"}',
		} as unknown as Response);
		vi.stubGlobal("fetch", fetchMock);

		const client = new PacaAPIClient(bearerConfig);

		await expect(client.listProjects()).rejects.toThrow(/do NOT retry/);
		expect(fetchMock).toHaveBeenCalledTimes(1);

		// The latch short-circuits before any network call — no retry storm.
		await expect(client.listProjects()).rejects.toThrow(
			AuthTokenRejectedError,
		);
		expect(fetchMock).toHaveBeenCalledTimes(1);
	});

	it("apikey mode 401 keeps upstream error shape and does not latch", async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: false,
			status: 401,
			statusText: "Unauthorized",
			text: async () => "nope",
		} as unknown as Response);
		vi.stubGlobal("fetch", fetchMock);

		const client = new PacaAPIClient(apikeyConfig);

		await expect(client.listProjects()).rejects.toThrow(
			/API request failed: 401/,
		);
		// Not latched: the next call still goes to the network.
		await expect(client.listProjects()).rejects.toThrow(
			/API request failed: 401/,
		);
		expect(fetchMock).toHaveBeenCalledTimes(2);
	});
});
