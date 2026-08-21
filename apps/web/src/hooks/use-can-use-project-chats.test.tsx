import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
	useCanStartTaskChat,
	useCanUseProjectChats,
	useProjectChatPermissions,
} from "./use-can-use-project-chats";

const permissionState = vi.hoisted(() => ({
	global: new Set<string>(),
	project: new Set<string>(),
}));

vi.mock("./use-permissions", () => ({
	usePermissions: () => ({
		hasPermission: (permission: string) =>
			permissionState.global.has(permission),
	}),
}));

vi.mock("./use-project-permissions", () => ({
	useProjectPermissions: () => ({
		hasProjectPermission: (permission: string) =>
			permissionState.project.has(permission),
	}),
}));

describe("useCanUseProjectChats", () => {
	beforeEach(() => {
		permissionState.global.clear();
		permissionState.project.clear();
	});

	it("allows Chats for global agents.read", () => {
		permissionState.global.add("agents.read");
		const { result } = renderHook(() => useCanUseProjectChats("project-1"));
		expect(result.current).toBe(true);
	});

	it("allows Chats for project-scoped agents.read", () => {
		permissionState.project.add("agents.read");
		const { result } = renderHook(() => useCanUseProjectChats("project-1"));
		expect(result.current).toBe(true);
	});

	it("hides Chats when neither permission grants access", () => {
		const { result } = renderHook(() => useCanUseProjectChats("project-1"));
		expect(result.current).toBe(false);
	});

	it("requires both agents.read and tasks.read for task entry", () => {
		permissionState.global.add("agents.read");
		const { result, rerender } = renderHook(() =>
			useCanStartTaskChat("project-1"),
		);
		expect(result.current).toBe(false);

		permissionState.project.add("tasks.read");
		rerender();
		expect(result.current).toBe(true);
	});

	it("requires tasks.write in addition to read permissions for publication", () => {
		permissionState.global.add("agents.read");
		permissionState.project.add("tasks.read");
		const { result, rerender } = renderHook(() =>
			useProjectChatPermissions("project-1"),
		);
		expect(result.current.canUseTaskContext).toBe(true);
		expect(result.current.canPublishConclusion).toBe(false);

		permissionState.project.add("tasks.write");
		rerender();
		expect(result.current.canPublishConclusion).toBe(true);
	});
});
