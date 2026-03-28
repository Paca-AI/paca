import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useIsDark } from "./use-is-dark";

describe("useIsDark", () => {
	it("reads initial dark mode from document class list", () => {
		document.documentElement.className = "dark";

		const { result } = renderHook(() => useIsDark());

		expect(result.current).toBe(true);
	});

	it("updates when root class changes", async () => {
		document.documentElement.className = "light";
		const { result } = renderHook(() => useIsDark());
		expect(result.current).toBe(false);

		await act(async () => {
			document.documentElement.className = "dark";
			await Promise.resolve();
		});

		expect(result.current).toBe(true);
	});
});
