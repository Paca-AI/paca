import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { loadPendingMfa, savePendingMfa } from "@/lib/mfa-challenge";
import { renderWithQueries } from "@/test/render-with-queries";
import { MfaChallengePanel } from "./MfaChallengePanel";

const mocks = vi.hoisted(() => ({ navigate: vi.fn(), post: vi.fn() }));
vi.mock("@tanstack/react-router", () => ({
	useNavigate: () => mocks.navigate,
}));
vi.mock("@/lib/api-client", () => ({
	apiClient: { instance: { post: mocks.post } },
}));

const pending = { challenge: "chal" };

beforeEach(() => {
	vi.clearAllMocks();
	sessionStorage.clear();
	savePendingMfa(pending);
	mocks.navigate.mockResolvedValue(undefined);
});

describe("MfaChallengePanel", () => {
	it("verifies the code against the challenge, then opens the app", async () => {
		mocks.post.mockResolvedValue({ data: {} });
		const user = userEvent.setup();
		renderWithQueries(<MfaChallengePanel pending={pending} />);

		await user.type(screen.getByLabelText(/authentication code/i), "123456");
		await user.click(screen.getByRole("button", { name: /^verify$/i }));

		await waitFor(() =>
			expect(mocks.post).toHaveBeenCalledWith("/auth/mfa/verify", {
				challenge: "chal",
				code: "123456",
			}),
		);
		await waitFor(() =>
			expect(mocks.navigate).toHaveBeenCalledWith({ to: "/home" }),
		);
		expect(loadPendingMfa()).toBeNull();
	});

	it("keeps the page and the challenge after a wrong code", async () => {
		mocks.post.mockRejectedValue({
			response: { data: { error_code: "AUTH_MFA_INVALID_CODE" } },
		});
		const user = userEvent.setup();
		renderWithQueries(<MfaChallengePanel pending={pending} />);

		await user.type(screen.getByLabelText(/authentication code/i), "000000");
		await user.click(screen.getByRole("button", { name: /^verify$/i }));

		expect(await screen.findByRole("alert")).toHaveTextContent(
			/invalid or was already used/i,
		);
		expect(mocks.navigate).not.toHaveBeenCalled();
		expect(loadPendingMfa()).not.toBeNull();
	});

	it("sends the user back to sign in when the challenge has expired", async () => {
		mocks.post.mockRejectedValue({
			response: { data: { error_code: "AUTH_TOKEN_INVALID" } },
		});
		const user = userEvent.setup();
		renderWithQueries(<MfaChallengePanel pending={pending} />);

		await user.type(screen.getByLabelText(/authentication code/i), "123456");
		await user.click(screen.getByRole("button", { name: /^verify$/i }));

		await waitFor(() =>
			expect(mocks.navigate).toHaveBeenCalledWith({ to: "/", search: {} }),
		);
		expect(loadPendingMfa()).toBeNull();
	});

	it("does not submit an empty code", () => {
		renderWithQueries(<MfaChallengePanel pending={pending} />);
		expect(screen.getByRole("button", { name: /^verify$/i })).toBeDisabled();
	});
});
