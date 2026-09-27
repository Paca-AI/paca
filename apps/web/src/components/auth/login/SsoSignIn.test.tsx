import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { SsoErrorNotice } from "./SsoSignIn";

describe("SsoErrorNotice", () => {
	it("renders nothing without an error code", () => {
		render(<SsoErrorNotice />);
		expect(screen.queryByRole("alert")).toBeNull();
	});

	it("explains a known error code", () => {
		render(<SsoErrorNotice code="no_account" />);
		expect(screen.getByRole("alert")).toHaveTextContent(
			"There is no Paca account for this identity.",
		);
	});

	it("falls back to a generic message for an unknown code", () => {
		render(<SsoErrorNotice code="<script>" />);
		expect(screen.getByRole("alert")).toHaveTextContent(
			"Single sign-on failed. Please try again.",
		);
	});
});
