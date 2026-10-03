import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { api, state } = vi.hoisted(() => ({
	api: {
		requestProjectExport: vi.fn(),
		getExportDownloadUrl: vi.fn(),
		triggerDownload: vi.fn(),
	},
	state: { exports: [] as unknown[] },
}));

vi.mock("@/lib/export-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/export-api")>(
			"@/lib/export-api",
		);
	return {
		...actual,
		requestProjectExport: api.requestProjectExport,
		getExportDownloadUrl: api.getExportDownloadUrl,
		triggerDownload: api.triggerDownload,
		projectExportsQueryOptions: (projectId: string) => ({
			queryKey: actual.projectExportsQueryKey(projectId),
			queryFn: async () => state.exports,
		}),
	};
});

import { ApiErrorCode } from "@/lib/api-error";
import type { ProjectExport } from "@/lib/export-api";
import { renderWithQueries } from "@/test/render-with-queries";
import { ExportSettings } from "./ExportSettings";

function makeExport(over: Partial<ProjectExport> = {}): ProjectExport {
	return {
		id: "e1",
		project_id: "p1",
		kind: "project_archive",
		status: "completed",
		file_name: "Paca-export-2026-10-03.zip",
		file_size: 2048,
		row_count: 42,
		created_at: "2026-10-03T10:00:00.000Z",
		completed_at: "2026-10-03T10:00:05.000Z",
		expires_at: "2026-10-10T10:00:05.000Z",
		expired: false,
		...over,
	};
}

function apiError(code: string) {
	return { response: { status: 409, data: { error_code: code } } };
}

beforeEach(() => {
	vi.clearAllMocks();
	state.exports = [];
});

describe("ExportSettings", () => {
	it("shows a no-permission state and fetches nothing without project.export", () => {
		renderWithQueries(<ExportSettings projectId="p1" canExport={false} />);

		expect(
			screen.getByText("You don't have permission to export this project"),
		).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Export project" }),
		).not.toBeInTheDocument();
	});

	it("queues an export when the button is clicked", async () => {
		api.requestProjectExport.mockResolvedValue(
			makeExport({ status: "pending", file_name: undefined }),
		);
		renderWithQueries(<ExportSettings projectId="p1" canExport />);

		await userEvent.click(
			await screen.findByRole("button", { name: "Export project" }),
		);

		await waitFor(() =>
			expect(api.requestProjectExport).toHaveBeenCalledWith("p1"),
		);
	});

	it("disables the button while an export is queued or running", async () => {
		state.exports = [makeExport({ status: "processing" })];
		renderWithQueries(<ExportSettings projectId="p1" canExport />);

		expect(await screen.findByText("Generating…")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Exporting…" })).toBeDisabled();
	});

	it("explains when another export is already in progress", async () => {
		api.requestProjectExport.mockRejectedValue(
			apiError(ApiErrorCode.ProjectExportInProgress),
		);
		renderWithQueries(<ExportSettings projectId="p1" canExport />);

		await userEvent.click(
			await screen.findByRole("button", { name: "Export project" }),
		);

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"An export is already in progress for this project.",
		);
	});

	it("lists a completed export with its details and downloads it", async () => {
		state.exports = [makeExport()];
		api.getExportDownloadUrl.mockResolvedValue("https://files.example/x.csv");
		renderWithQueries(<ExportSettings projectId="p1" canExport />);

		expect(
			await screen.findByText("Paca-export-2026-10-03.zip"),
		).toBeInTheDocument();
		expect(screen.getByText("Ready")).toBeInTheDocument();
		expect(screen.getByText(/Tasks: 42/)).toBeInTheDocument();
		expect(screen.getByText(/2\.0 KB/)).toBeInTheDocument();

		await userEvent.click(screen.getByRole("button", { name: "Download" }));

		await waitFor(() =>
			expect(api.triggerDownload).toHaveBeenCalledWith(
				"https://files.example/x.csv",
			),
		);
		expect(api.getExportDownloadUrl).toHaveBeenCalledWith("p1", "e1");
	});

	it("offers no download for an expired export", async () => {
		state.exports = [makeExport({ expired: true })];
		renderWithQueries(<ExportSettings projectId="p1" canExport />);

		expect(await screen.findByText("Expired")).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Download" }),
		).not.toBeInTheDocument();
	});

	it("shows a failed export without a download and without the raw server error", async () => {
		state.exports = [
			makeExport({
				status: "failed",
				file_name: undefined,
				error_message: "internal detail that should not be shown",
			}),
		];
		renderWithQueries(<ExportSettings projectId="p1" canExport />);

		expect(await screen.findByText("Failed")).toBeInTheDocument();
		expect(
			screen.getByText("The export could not be generated. Please try again."),
		).toBeInTheDocument();
		expect(screen.queryByText(/internal detail/)).not.toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Download" }),
		).not.toBeInTheDocument();
	});

	it("reports a failed download", async () => {
		state.exports = [makeExport()];
		api.getExportDownloadUrl.mockRejectedValue(new Error("boom"));
		renderWithQueries(<ExportSettings projectId="p1" canExport />);

		await userEvent.click(
			await screen.findByRole("button", { name: "Download" }),
		);

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"Could not download the export. Please try again.",
		);
		expect(api.triggerDownload).not.toHaveBeenCalled();
	});
});
