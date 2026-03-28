import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ThemeToggle from "./ThemeToggle";

type MediaQueryListener = (event: MediaQueryListEvent) => void;

function mockMatchMedia(prefersDark: boolean) {
	let listeners: MediaQueryListener[] = [];

	Object.defineProperty(window, "matchMedia", {
		writable: true,
		value: vi.fn().mockImplementation((query: string) => ({
			matches: query === "(prefers-color-scheme: dark)" ? prefersDark : false,
			media: query,
			onchange: null,
			addEventListener: (_event: string, listener: MediaQueryListener) => {
				listeners.push(listener);
			},
			removeEventListener: (_event: string, listener: MediaQueryListener) => {
				listeners = listeners.filter((item) => item !== listener);
			},
			dispatchEvent: () => true,
		})),
	});

	return {
		emitChange(nextPrefersDark: boolean) {
			prefersDark = nextPrefersDark;
			const event = {
				matches: nextPrefersDark,
				media: "(prefers-color-scheme: dark)",
			} as MediaQueryListEvent;
			for (const listener of listeners) {
				listener(event);
			}
		},
	};
}

describe("ThemeToggle", () => {
	beforeEach(() => {
		window.localStorage.clear();
		document.documentElement.className = "";
		document.documentElement.removeAttribute("data-theme");
		document.documentElement.style.colorScheme = "";
		mockMatchMedia(false);
	});

	it("cycles through light, dark, and auto", async () => {
		render(<ThemeToggle />);
		const user = userEvent.setup();

		const button = screen.getByRole("button", { name: /theme mode/i });
		expect(button).toHaveTextContent("Auto");

		await user.click(button);
		expect(button).toHaveTextContent("Light");
		expect(window.localStorage.getItem("theme")).toBe("light");
		expect(document.documentElement).toHaveClass("light");
		expect(document.documentElement.getAttribute("data-theme")).toBe("light");

		await user.click(button);
		expect(button).toHaveTextContent("Dark");
		expect(window.localStorage.getItem("theme")).toBe("dark");
		expect(document.documentElement).toHaveClass("dark");
		expect(document.documentElement.getAttribute("data-theme")).toBe("dark");

		await user.click(button);
		expect(button).toHaveTextContent("Auto");
		expect(window.localStorage.getItem("theme")).toBe("auto");
		expect(document.documentElement.getAttribute("data-theme")).toBeNull();
	});

	it("responds to system theme change when in auto mode", async () => {
		const media = mockMatchMedia(false);
		render(<ThemeToggle />);

		expect(document.documentElement.className).toBe("");
		act(() => {
			media.emitChange(true);
		});

		await vi.waitFor(() => {
			expect(document.documentElement).toHaveClass("dark");
		});
	});
});
