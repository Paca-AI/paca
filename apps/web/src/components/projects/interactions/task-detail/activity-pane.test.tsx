import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Activity } from "@/lib/interaction-api";
import {
	shouldDisplayTaskActivity,
	useLoadAllConclusionPages,
} from "./activity-pane";

describe("useLoadAllConclusionPages", () => {
	it("loads every available page in sequence", () => {
		const fetchNextPage = vi.fn().mockResolvedValue(undefined);
		const { rerender } = renderHook(
			(props) => useLoadAllConclusionPages(props),
			{
				initialProps: {
					hasNextPage: true,
					isFetchingNextPage: false,
					isFetchNextPageError: false,
					fetchNextPage,
				},
			},
		);

		expect(fetchNextPage).toHaveBeenCalledTimes(1);
		rerender({
			hasNextPage: true,
			isFetchingNextPage: true,
			isFetchNextPageError: false,
			fetchNextPage,
		});
		rerender({
			hasNextPage: true,
			isFetchingNextPage: false,
			isFetchNextPageError: false,
			fetchNextPage,
		});
		expect(fetchNextPage).toHaveBeenCalledTimes(2);
		rerender({
			hasNextPage: false,
			isFetchingNextPage: false,
			isFetchNextPageError: false,
			fetchNextPage,
		});
		expect(fetchNextPage).toHaveBeenCalledTimes(2);
	});

	it("stops automatic pagination after a persistent next-page failure", () => {
		const fetchNextPage = vi.fn().mockResolvedValue(undefined);
		const { rerender } = renderHook(
			(props) => useLoadAllConclusionPages(props),
			{
				initialProps: {
					hasNextPage: true,
					isFetchingNextPage: false,
					isFetchNextPageError: false,
					fetchNextPage,
				},
			},
		);

		expect(fetchNextPage).toHaveBeenCalledTimes(1);
		rerender({
			hasNextPage: true,
			isFetchingNextPage: false,
			isFetchNextPageError: true,
			fetchNextPage,
		});
		rerender({
			hasNextPage: true,
			isFetchingNextPage: false,
			isFetchNextPageError: true,
			fetchNextPage,
		});
		expect(fetchNextPage).toHaveBeenCalledTimes(1);
	});
});

describe("shouldDisplayTaskActivity", () => {
	it("hides the obsolete conclusion projection for legacy description writebacks", () => {
		const activity = (
			activityType: string,
			content: Record<string, unknown>,
		): Activity => ({
			id: `activity-${activityType}`,
			task_id: "task-1",
			actor_name: "Agent",
			actor_username: "agent",
			activity_type: activityType,
			content,
			created_at: "2026-08-19T00:00:00Z",
			updated_at: "2026-08-19T00:00:00Z",
		});
		const legacyDescriptionConclusion = activity("agent.conclusion.published", {
			publication_id: "publication-1",
			description_updated: true,
		});
		const summaryOnlyConclusion = activity("agent.conclusion.published", {
			publication_id: "publication-2",
			description_updated: false,
		});
		const descriptionUpdate = activity("task.updated", {
			conclusion_publication_id: "publication-1",
		});

		expect(shouldDisplayTaskActivity(legacyDescriptionConclusion)).toBe(false);
		expect(shouldDisplayTaskActivity(summaryOnlyConclusion)).toBe(true);
		expect(shouldDisplayTaskActivity(descriptionUpdate)).toBe(true);
	});
});
