import { CatchBoundary } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { RouteErrorComponent } from "./route-error-boundary";

function ThrowingComponent({ error }: { error: unknown }): never {
	throw error;
}

describe("RouteErrorComponent", () => {
	it("shows a permission-denied message and no Retry button for a 403", () => {
		const error = Object.assign(new Error("insufficient permissions"), {
			response: { status: 403, data: { error_code: "FORBIDDEN" } },
		});

		render(<RouteErrorComponent error={error as Error} />);

		expect(
			screen.getByText(/you don't have permission to view this/i),
		).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /retry/i }),
		).not.toBeInTheDocument();
		// The raw "insufficient permissions" server message adds nothing beyond
		// the title above, so it's deliberately not shown for this error kind.
		expect(
			screen.queryByText(/insufficient permissions/i),
		).not.toBeInTheDocument();
	});

	it("does not treat AUTH_PASSWORD_CHANGE_REQUIRED as a generic permission error", () => {
		const error = Object.assign(new Error("must change password"), {
			response: {
				status: 403,
				data: { error_code: "AUTH_PASSWORD_CHANGE_REQUIRED" },
			},
		});

		render(<RouteErrorComponent error={error as Error} />);

		expect(
			screen.queryByText(/you don't have permission to view this/i),
		).not.toBeInTheDocument();
		expect(screen.getByText(/something went wrong/i)).toBeInTheDocument();
	});

	it("shows a not-found message for a 404", () => {
		const error = Object.assign(new Error("task not found"), {
			response: { status: 404, data: { error_code: "TASK_NOT_FOUND" } },
		});

		render(<RouteErrorComponent error={error as Error} />);

		expect(screen.getByText("Not found")).toBeInTheDocument();
	});

	it("shows a generic message with a Retry button for other errors", () => {
		const error = Object.assign(new Error("internal error"), {
			response: { status: 500, data: {} },
		});

		render(<RouteErrorComponent error={error as Error} />);

		expect(screen.getByText(/something went wrong/i)).toBeInTheDocument();
		expect(screen.getByText(/internal error/i)).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
	});
});

describe("RouteErrorComponent wrapped in CatchBoundary", () => {
	// Regression coverage for src/routes/_authenticated.tsx's fix: a route's
	// `errorComponent` boundary (installed via `Route.options.errorComponent`)
	// replaces that *entire* route's own rendered output when a descendant
	// throws, sidebar and all — because the boundary wraps the whole
	// component, not just its own <Outlet/>. The fix wraps only the routed
	// content by using `CatchBoundary` (the same primitive TanStack Router's
	// own errorComponent machinery is built on, re-exported publicly) inside
	// the layout's own JSX, positioned as a sibling to the sidebar rather
	// than an ancestor of it. This proves that placement actually preserves
	// a sibling instead of also erasing it — the one thing a route-level
	// `errorComponent` cannot do.
	it("keeps a sibling outside the boundary while replacing only the thrown child", () => {
		const error = Object.assign(new Error("insufficient permissions"), {
			response: { status: 403, data: { error_code: "FORBIDDEN" } },
		});
		const onError = vi.fn();
		const consoleError = vi
			.spyOn(console, "error")
			.mockImplementation(() => {});

		render(
			<div>
				<nav data-testid="sidebar">Sidebar</nav>
				<CatchBoundary
					getResetKey={() => "test"}
					errorComponent={RouteErrorComponent}
					onCatch={onError}
				>
					<ThrowingComponent error={error} />
				</CatchBoundary>
			</div>,
		);

		expect(screen.getByTestId("sidebar")).toBeInTheDocument();
		expect(
			screen.getByText(/you don't have permission to view this/i),
		).toBeInTheDocument();
		expect(onError).toHaveBeenCalled();

		consoleError.mockRestore();
	});
});
