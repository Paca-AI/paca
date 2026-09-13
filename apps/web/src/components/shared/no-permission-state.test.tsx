import { render, screen } from "@testing-library/react";
import { Shield } from "lucide-react";
import { describe, expect, it } from "vitest";

import { NoPermissionState } from "./no-permission-state";

describe("NoPermissionState", () => {
	it("renders the given title and description", () => {
		render(<NoPermissionState title="No access" description="Ask an admin." />);

		expect(screen.getByText("No access")).toBeInTheDocument();
		expect(screen.getByText("Ask an admin.")).toBeInTheDocument();
	});

	it("omits the description paragraph when none is given", () => {
		const { container } = render(<NoPermissionState title="No access" />);

		expect(screen.getByText("No access")).toBeInTheDocument();
		expect(container.querySelectorAll("p")).toHaveLength(1);
	});

	it("renders the given icon", () => {
		const { container } = render(
			<NoPermissionState icon={Shield} title="No access" />,
		);

		expect(container.querySelector("svg")).toBeInTheDocument();
	});
});
