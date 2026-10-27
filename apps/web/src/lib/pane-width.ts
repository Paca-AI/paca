import { useCallback, useEffect, useSyncExternalStore } from "react";

export interface PaneWidthConfig {
	/** localStorage key the width is persisted under. */
	storageKey: string;
	defaultWidth: number;
	minWidth: number;
	/** Hard cap; the pane may also never exceed `maxViewportRatio` of the window. */
	maxWidth: number;
	maxViewportRatio: number;
	keyStep: number;
}

export const SIDEBAR_PANE: PaneWidthConfig = {
	storageKey: "paca:sidebar-width",
	defaultWidth: 256,
	minWidth: 200,
	maxWidth: 480,
	maxViewportRatio: 0.4,
	keyStep: 16,
};

export const ROADMAP_TASK_PANE: PaneWidthConfig = {
	storageKey: "paca:roadmap-task-column-width",
	defaultWidth: 280,
	minWidth: 200,
	maxWidth: 640,
	maxViewportRatio: 0.6,
	keyStep: 16,
};

export function maxPaneWidth(
	config: PaneWidthConfig,
	viewportWidth: number,
): number {
	return Math.max(
		config.minWidth,
		Math.min(config.maxWidth, viewportWidth * config.maxViewportRatio),
	);
}

export function clampPaneWidth(
	config: PaneWidthConfig,
	width: number,
	viewportWidth: number,
): number {
	const max = maxPaneWidth(config, viewportWidth);
	return Math.round(Math.max(config.minWidth, Math.min(width, max)));
}

export function readStoredPaneWidth(
	config: PaneWidthConfig,
	viewportWidth: number,
): number {
	try {
		const raw = localStorage.getItem(config.storageKey);
		const stored = raw === null ? Number.NaN : Number(raw);
		return Number.isFinite(stored) && stored > 0
			? clampPaneWidth(config, stored, viewportWidth)
			: config.defaultWidth;
	} catch {
		return config.defaultWidth;
	}
}

export function storePaneWidth(config: PaneWidthConfig, width: number) {
	try {
		if (width === config.defaultWidth) {
			localStorage.removeItem(config.storageKey);
		} else {
			localStorage.setItem(config.storageKey, String(width));
		}
	} catch {
		// Persisting the width is best-effort.
	}
}

// One shared store per storage key so every consumer of the same pane (e.g.
// the sidebar provider and the sidebar's own drag handle) sees one width.
interface PaneState {
	width: number;
	dragging: boolean;
	listeners: Set<() => void>;
}

const states = new Map<string, PaneState>();

function getState(config: PaneWidthConfig): PaneState {
	let state = states.get(config.storageKey);
	if (!state) {
		state = {
			width: readStoredPaneWidth(config, window.innerWidth),
			dragging: false,
			listeners: new Set(),
		};
		states.set(config.storageKey, state);
	}
	return state;
}

function emit(state: PaneState) {
	for (const listener of state.listeners) listener();
}

export interface PaneWidth {
	width: number;
	maxWidth: number;
	dragging: boolean;
	/** Resize live (clamped); call `commit` to persist. */
	setWidth: (next: number) => void;
	setDragging: (dragging: boolean) => void;
	/** Persist the current width. */
	commit: () => void;
	reset: () => void;
}

export function usePaneWidth(config: PaneWidthConfig): PaneWidth {
	const state = getState(config);
	const subscribe = useCallback(
		(listener: () => void) => {
			state.listeners.add(listener);
			return () => state.listeners.delete(listener);
		},
		[state],
	);
	const width = useSyncExternalStore(subscribe, () => state.width);
	const dragging = useSyncExternalStore(subscribe, () => state.dragging);
	const viewportWidth = useSyncExternalStore(
		(listener) => {
			window.addEventListener("resize", listener);
			return () => window.removeEventListener("resize", listener);
		},
		() => window.innerWidth,
	);

	const setWidth = useCallback(
		(next: number) => {
			const clamped = clampPaneWidth(config, next, window.innerWidth);
			if (clamped === state.width) return;
			state.width = clamped;
			emit(state);
		},
		[config, state],
	);
	const setDragging = useCallback(
		(next: boolean) => {
			if (state.dragging === next) return;
			state.dragging = next;
			emit(state);
		},
		[state],
	);
	const commit = useCallback(
		() => storePaneWidth(config, state.width),
		[config, state],
	);
	const reset = useCallback(() => {
		setWidth(config.defaultWidth);
		storePaneWidth(config, state.width);
	}, [config, setWidth, state]);

	// Keep the in-memory width within bounds when the window shrinks.
	useEffect(() => {
		const clamped = clampPaneWidth(config, state.width, viewportWidth);
		if (clamped !== state.width) {
			state.width = clamped;
			emit(state);
		}
	}, [config, state, viewportWidth]);

	return {
		width,
		maxWidth: maxPaneWidth(config, viewportWidth),
		dragging,
		setWidth,
		setDragging,
		commit,
		reset,
	};
}
