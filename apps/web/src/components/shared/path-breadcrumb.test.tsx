import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { PathBreadcrumb, type PathCrumb } from "./path-breadcrumb";

const crumb = (key: string, extra: Partial<PathCrumb> = {}): PathCrumb => ({
	key,
	label: key,
	...extra,
});

describe("PathBreadcrumb", () => {
	it("opens an ancestor and shows the last crumb as the current page", () => {
		const onSelect = vi.fn();
		render(
			<PathBreadcrumb
				crumbs={[
					crumb("Project", { onSelect }),
					crumb("Docs", { onSelect }),
					crumb("Plan"),
				]}
			/>,
		);
		fireEvent.click(screen.getByRole("button", { name: "Docs" }));
		expect(onSelect).toHaveBeenCalledTimes(1);
		const current = screen.getByText("Plan");
		expect(current.getAttribute("aria-current")).toBe("page");
		expect(screen.queryByRole("button", { name: "Plan" })).toBeNull();
	});

	it("makes a crumb a drop target for the dragged ids", () => {
		const onDrop = vi.fn();
		render(
			<PathBreadcrumb
				crumbs={[
					crumb("Root", {
						onSelect: () => undefined,
						drop: { mime: "application/x-test", onDrop },
					}),
					crumb("Here"),
				]}
			/>,
		);
		const target = screen.getByRole("button", { name: "Root" });
		const data = { "application/x-test": JSON.stringify(["a", "b"]) };
		const dataTransfer = {
			types: Object.keys(data),
			getData: (type: string) => data[type as keyof typeof data] ?? "",
		};
		fireEvent.dragOver(target, { dataTransfer });
		fireEvent.drop(target, { dataTransfer });
		expect(onDrop).toHaveBeenCalledWith(["a", "b"]);
	});

	it("ignores a drag that is not its payload", () => {
		const onDrop = vi.fn();
		render(
			<PathBreadcrumb
				crumbs={[
					crumb("Root", {
						onSelect: () => undefined,
						drop: { mime: "application/x-test", onDrop },
					}),
					crumb("Here"),
				]}
			/>,
		);
		const target = screen.getByRole("button", { name: "Root" });
		fireEvent.drop(target, {
			dataTransfer: { types: ["text/plain"], getData: () => "" },
		});
		expect(onDrop).not.toHaveBeenCalled();
	});

	it("folds the middle of a long path into a menu, keeping first and last two", () => {
		render(
			<PathBreadcrumb
				crumbs={["A", "B", "C", "D", "E", "F"].map((k) => crumb(k))}
			/>,
		);
		expect(screen.getByText("A")).toBeTruthy();
		expect(screen.getByText("E")).toBeTruthy();
		expect(screen.getByText("F")).toBeTruthy();
		expect(screen.queryByText("B")).toBeNull();
		expect(screen.queryByText("C")).toBeNull();
	});
});
