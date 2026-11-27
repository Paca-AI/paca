import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ContentDiff } from "./content-diff";

function documentWithLink(text: string) {
	return [
		{
			type: "paragraph",
			content: [
				{
					type: "link",
					href: "https://example.com/docs",
					content: [{ type: "text", text, styles: {} }],
				},
			],
		},
	];
}

describe("ContentDiff link labels", () => {
	it("shows changed labels instead of reporting no differences", () => {
		const { container } = render(
			<ContentDiff
				oldContent={documentWithLink("Old guide")}
				newContent={documentWithLink("New guide")}
			/>,
		);
		expect(screen.getByText("Old guide")).toBeInTheDocument();
		expect(screen.getByText("New guide")).toBeInTheDocument();
		expect(container.textContent).toContain("-");
		expect(container.textContent).toContain("+");
	});

	it("shows deleted linked text", () => {
		render(
			<ContentDiff
				oldContent={documentWithLink("删除这段说明")}
				newContent={[]}
			/>,
		);
		expect(screen.getByText("删除这段说明")).toBeInTheDocument();
	});
});

describe("ContentDiff table cells", () => {
	it("shows an edited table row instead of reporting no differences", () => {
		const documentWithTable = (text: string) => [
			{
				type: "table",
				content: {
					type: "tableContent",
					rows: [
						{
							cells: [
								{
									type: "tableCell",
									content: [{ type: "text", text, styles: {} }],
								},
							],
						},
					],
				},
			},
		];
		const { container } = render(
			<ContentDiff
				oldContent={documentWithTable("Pending review")}
				newContent={documentWithTable("Review complete")}
			/>,
		);
		expect(screen.getByText("Pending review")).toBeInTheDocument();
		expect(screen.getByText("Review complete")).toBeInTheDocument();
		expect(container.textContent).toContain("-");
		expect(container.textContent).toContain("+");
	});
});
