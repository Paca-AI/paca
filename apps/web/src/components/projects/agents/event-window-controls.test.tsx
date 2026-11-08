import { act, render, screen } from "@testing-library/react";
import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const viewport = vi.hoisted(() => ({
	state: { isAtBottom: true, scrollToBottom: vi.fn() },
}));

vi.mock("@assistant-ui/react", () => ({
	useThreadViewport: (selector: (s: typeof viewport.state) => unknown) =>
		selector(viewport.state),
}));
vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (k: string) => k }),
}));

import { TailFollowIndicator } from "./event-window-controls";

function Harness({
	initialFollowing = true,
	newBelow = 0,
	expose,
}: {
	initialFollowing?: boolean;
	newBelow?: number;
	expose: (api: { jump: () => void; following: () => boolean }) => void;
}) {
	const [following, setFollowing] = useState(initialFollowing);
	const [jumpCount, setJumpCount] = useState(0);
	const jump = () => {
		setFollowing(true);
		setJumpCount((n) => n + 1);
	};
	expose({ jump, following: () => following });
	return (
		<>
			<span data-testid="following">{String(following)}</span>
			<TailFollowIndicator
				newBelow={newBelow}
				setFollowing={setFollowing}
				jumpToLatest={jump}
				jumpCount={jumpCount}
			/>
		</>
	);
}

describe("TailFollowIndicator", () => {
	beforeEach(() => {
		viewport.state = { isAtBottom: true, scrollToBottom: vi.fn() };
	});

	it("does not scroll on mount", () => {
		render(<Harness expose={() => {}} />);
		expect(viewport.state.scrollToBottom).not.toHaveBeenCalled();
	});

	it("keeps following after jumpToLatest while the viewport is not at the bottom", () => {
		viewport.state.isAtBottom = false;
		let api!: { jump: () => void };
		render(<Harness initialFollowing={false} expose={(a) => (api = a)} />);
		expect(screen.getByTestId("following").textContent).toBe("false");

		act(() => api.jump());

		expect(screen.getByTestId("following").textContent).toBe("true");
		expect(viewport.state.scrollToBottom).toHaveBeenCalledTimes(1);
		expect(viewport.state.scrollToBottom).toHaveBeenCalledWith({
			behavior: "auto",
		});
	});

	it("mirrors viewport changes into following", () => {
		viewport.state.isAtBottom = true;
		const { rerender } = render(<Harness expose={() => {}} />);
		viewport.state = { ...viewport.state, isAtBottom: false };
		rerender(<Harness expose={() => {}} />);
		expect(screen.getByTestId("following").textContent).toBe("false");
	});

	it("scrolls once per jump when the pill is clicked", () => {
		viewport.state.isAtBottom = false;
		render(<Harness initialFollowing={false} newBelow={2} expose={() => {}} />);
		act(() => screen.getByRole("button").click());
		expect(viewport.state.scrollToBottom).toHaveBeenCalledTimes(1);
	});
});
