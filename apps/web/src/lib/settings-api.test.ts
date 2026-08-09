import { QueryClient } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockGet, mockPatch } = vi.hoisted(() => ({
	mockGet: vi.fn(),
	mockPatch: vi.fn(),
}));

vi.mock("./api-client", () => ({
	apiClient: {
		instance: {
			get: mockGet,
			patch: mockPatch,
		},
	},
}));

import {
	type BrandingResponse,
	brandingQueryOptions,
	getBranding,
	setBrandingQueryData,
} from "./settings-api";

const CACHE_KEY = "paca:branding-cache";

describe("getBranding", () => {
	beforeEach(() => {
		mockGet.mockReset();
		window.localStorage.clear();
	});

	it("caches the fetched response in localStorage", async () => {
		const branding: BrandingResponse = { brand_name: "Acme" };
		mockGet.mockResolvedValue({ data: { data: branding } });

		await getBranding();

		expect(
			JSON.parse(window.localStorage.getItem(CACHE_KEY) ?? "null"),
		).toEqual(branding);
	});
});

describe("setBrandingQueryData", () => {
	beforeEach(() => {
		window.localStorage.clear();
	});

	function newQueryClientWith(initial: BrandingResponse) {
		const queryClient = new QueryClient();
		queryClient.setQueryData(brandingQueryOptions.queryKey, initial);
		return queryClient;
	}

	it("patches the React Query cache", () => {
		const queryClient = newQueryClientWith({ brand_name: "Old Name" });

		setBrandingQueryData(queryClient, (old) => ({
			...old,
			brand_name: "New Name",
		}));

		expect(
			queryClient.getQueryData<BrandingResponse>(brandingQueryOptions.queryKey),
		).toEqual({ brand_name: "New Name" });
	});

	// Regression test: after a logo/favicon upload or a brand-name/color save,
	// BrandingSettings.tsx previously patched only the React Query cache via
	// queryClient.setQueryData directly. The localStorage cache written by
	// getBranding() was left holding the pre-change snapshot, so a hard
	// reload immediately after such a change would briefly repaint the old
	// branding for one round-trip. setBrandingQueryData must keep both in
	// sync.
	it("also writes the patched value to the localStorage cache", () => {
		const queryClient = newQueryClientWith({
			brand_name: "Old Name",
			logo_url: "https://old-logo",
		});

		setBrandingQueryData(queryClient, (old) => ({
			...old,
			logo_url: "https://new-logo",
		}));

		expect(
			JSON.parse(window.localStorage.getItem(CACHE_KEY) ?? "null"),
		).toEqual({ brand_name: "Old Name", logo_url: "https://new-logo" });
	});

	it("does not write to localStorage when there is no cached value to patch", () => {
		const queryClient = new QueryClient(); // no initial branding data set

		// Mirrors BrandingSettings.tsx's updateImageCache: `old ? {...} : old`
		// returns `old` (undefined here) when there's no cached value yet to
		// merge into — must not overwrite localStorage with "undefined".
		setBrandingQueryData(queryClient, (old) => old);

		expect(window.localStorage.getItem(CACHE_KEY)).toBeNull();
		expect(
			queryClient.getQueryData<BrandingResponse>(brandingQueryOptions.queryKey),
		).toBeUndefined();
	});
});
