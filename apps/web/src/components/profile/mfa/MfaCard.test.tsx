import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { renderWithQueries } from "@/test/render-with-queries";
import { MfaCard } from "./MfaCard";

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock("@/lib/api-client", () => ({ apiClient: { instance: http } }));

function status(enabled: boolean, left = 8) {
	http.get.mockResolvedValue({
		data: { data: { enabled, recovery_codes_left: left } },
	});
}

beforeEach(() => vi.clearAllMocks());

describe("MfaCard", () => {
	it("offers setup when two-step verification is off", async () => {
		status(false);
		renderWithQueries(<MfaCard />);
		expect(
			await screen.findByRole("button", { name: /set up two-step/i }),
		).toBeInTheDocument();
		expect(screen.queryByText(/recovery codes remaining/i)).toBeNull();
	});

	it("shows recovery codes left and warns when they run low", async () => {
		status(true, 1);
		renderWithQueries(<MfaCard />);
		expect(
			await screen.findByText(/recovery codes remaining: 1/i),
		).toBeInTheDocument();
		expect(screen.getByText(/running low/i)).toBeInTheDocument();
	});

	it("walks through setup: scan, verify a code, then keep the recovery codes", async () => {
		status(false);
		http.post.mockImplementation(async (url: string) => {
			if (url === "/users/me/mfa/enroll") {
				return {
					data: {
						data: {
							secret: "JBSWY3DPEHPK3PXP",
							uri: "otpauth://totp/Paca:alice?secret=JBSWY3DPEHPK3PXP",
						},
					},
				};
			}
			return {
				data: { data: { recovery_codes: ["abcde-fghjk", "mnpqr-stuvw"] } },
			};
		});
		const user = userEvent.setup();
		renderWithQueries(<MfaCard />);

		await user.click(
			await screen.findByRole("button", { name: /set up two-step/i }),
		);
		expect(await screen.findByText("JBSWY3DPEHPK3PXP")).toBeInTheDocument();
		expect(screen.getByRole("img", { name: /qr code/i })).toBeInTheDocument();

		await user.type(screen.getByLabelText(/6-digit code/i), "123456");
		await user.click(
			screen.getByRole("button", { name: /verify and turn on/i }),
		);

		expect(await screen.findByText("abcde-fghjk")).toBeInTheDocument();
		expect(http.post).toHaveBeenCalledWith("/users/me/mfa/confirm", {
			code: "123456",
		});
	});

	it("reports a wrong code without leaving the step", async () => {
		status(false);
		http.post.mockImplementation(async (url: string) => {
			if (url === "/users/me/mfa/enroll") {
				return {
					data: { data: { secret: "ABC", uri: "otpauth://totp/x?secret=ABC" } },
				};
			}
			throw { response: { data: { error_code: "AUTH_MFA_INVALID_CODE" } } };
		});
		const user = userEvent.setup();
		renderWithQueries(<MfaCard />);

		await user.click(
			await screen.findByRole("button", { name: /set up two-step/i }),
		);
		await user.type(await screen.findByLabelText(/6-digit code/i), "000000");
		await user.click(
			screen.getByRole("button", { name: /verify and turn on/i }),
		);

		await waitFor(() =>
			expect(screen.getByRole("alert")).toHaveTextContent(
				/invalid or was already used/i,
			),
		);
		expect(screen.queryByText(/save your recovery codes/i)).toBeNull();
	});

	it("needs a current code to turn two-step verification off", async () => {
		status(true);
		http.post.mockResolvedValue({ data: {} });
		const user = userEvent.setup();
		renderWithQueries(<MfaCard />);

		await user.click(
			await screen.findByRole("button", { name: /^turn off$/i }),
		);
		const confirm = screen.getByRole("button", { name: /^turn off$/i });
		expect(confirm).toBeDisabled();
		await user.type(
			screen.getByLabelText(/authentication code or recovery/i),
			"654321",
		);
		await user.click(confirm);

		await waitFor(() =>
			expect(http.post).toHaveBeenCalledWith("/users/me/mfa/disable", {
				code: "654321",
			}),
		);
	});
});
