import type { ReactNode } from "react";

/**
 * Display-only content for the many places that hand-roll their own sized
 * initials `<div>` instead of the `Avatar` primitive (task assignee stacks,
 * activity feed, team list, etc.) — drops into that existing wrapper without
 * touching its size/ring/gradient classes. Renders an image when avatarUrl
 * is set, otherwise the given fallback (initials text or an icon).
 */
export function EntityAvatarContent({
	avatarUrl,
	children,
}: {
	avatarUrl?: string | null;
	children: ReactNode;
}) {
	if (avatarUrl) {
		return (
			<img
				src={avatarUrl}
				alt=""
				className="size-full rounded-[inherit] object-cover"
			/>
		);
	}
	return <>{children}</>;
}
