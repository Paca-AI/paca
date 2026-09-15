import "@testing-library/jest-dom/vitest";
import "@/i18n/config";
import { beforeAll, beforeEach } from "vitest";

type StorageLike = {
	getItem: (key: string) => string | null;
	setItem: (key: string, value: string) => void;
	removeItem: (key: string) => void;
	clear: () => void;
};

function createStorageMock(): StorageLike {
	const store = new Map<string, string>();

	return {
		getItem: (key: string) => store.get(key) ?? null,
		setItem: (key: string, value: string) => {
			store.set(key, String(value));
		},
		removeItem: (key: string) => {
			store.delete(key);
		},
		clear: () => {
			store.clear();
		},
	};
}

beforeAll(() => {
	Object.defineProperty(window, "localStorage", {
		configurable: true,
		value: createStorageMock(),
	});
	// jsdom has no matchMedia implementation — any component that calls it
	// (useIsMobile, useThemeMode's prefers-color-scheme check, …) throws
	// "window.matchMedia is not a function" as soon as it mounts in a test
	// otherwise. Defaults every query to "no match" (desktop, light);
	// `configurable: true` so a test that needs specific results —
	// ThemeToggle.test.tsx's own mockMatchMedia — can still override it.
	Object.defineProperty(window, "matchMedia", {
		configurable: true,
		writable: true,
		value: (query: string) => ({
			matches: false,
			media: query,
			onchange: null,
			addListener: () => {},
			removeListener: () => {},
			addEventListener: () => {},
			removeEventListener: () => {},
			dispatchEvent: () => false,
		}),
	});
});

beforeEach(() => {
	window.localStorage.clear();
});
