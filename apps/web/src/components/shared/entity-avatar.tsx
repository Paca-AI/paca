import { User } from "lucide-react";
import type { ReactNode } from "react";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { getInitials } from "@/lib/initials";
import {
	type AgentProviderInfo,
	type AvatarSubject,
	PACA_AVATAR_DARK,
	PACA_AVATAR_LIGHT,
	resolveAvatarSrc,
} from "@/lib/provider-logos";
import { cn } from "@/lib/utils";

const SIZES = {
	xs: "size-5 text-[9px]",
	sm: "size-6 text-[10px]",
	md: "size-7 text-xs",
	default: "size-8 text-xs",
	lg: "size-10 text-sm",
	xl: "size-14 text-lg",
} as const;

// Full class strings (never built dynamically) so Tailwind can see them. The
// ring pseudo-element needs the radius too.
const RADII = {
	circle: { self: "rounded-full", ring: "after:rounded-full" },
	rounded: { self: "rounded-lg", ring: "after:rounded-lg" },
	inherit: { self: "rounded-[inherit]", ring: "after:rounded-[inherit]" },
} as const;

// The initials' font size per size. Set on the fallback itself, because
// AvatarFallback has its own text size that would otherwise win over one
// inherited from the root and let the initials spill out of a small avatar.
const FALLBACK_TEXT: Record<keyof typeof SIZES, string> = {
	xs: "text-[9px]",
	sm: "text-[10px]",
	md: "text-xs",
	default: "text-xs",
	lg: "text-sm",
	xl: "text-lg",
};

export type EntityAvatarSize = keyof typeof SIZES;

export interface EntityAvatarProps {
	/** Display name; the source of the initials for a human. */
	name: string;
	/** Decides the fallback chain; defaults to "human". */
	type?: AvatarSubject;
	/** The account's own uploaded avatar, if any. */
	avatarUrl?: string | null;
	/** Provider fields for an agent's default (provider-logo) avatar. */
	agent?: AgentProviderInfo;
	size?: EntityAvatarSize;
	/** "circle" (default), "rounded" (a rounded square), or "inherit" to take
	 * the radius of the wrapper (see AvatarUpload). */
	shape?: "circle" | "rounded" | "inherit";
	/** Shown instead of initials when a human has no name; defaults to a user icon. */
	fallback?: ReactNode;
	/** An <AvatarBadge> overlaid on the avatar's corner. */
	badge?: ReactNode;
	className?: string;
}

/**
 * The one avatar component used everywhere in Paca. What it shows:
 *
 *   human            uploaded avatar -> initials
 *   agent            uploaded avatar -> provider logo -> Paca avatar
 *   service account  uploaded avatar -> Paca avatar
 *
 * The chain itself lives in lib/provider-logos.ts (resolveAvatarSrc); pass a
 * record through one of its adapters (agentAvatar, memberAvatar, ...) rather
 * than assembling props by hand:
 *
 *   <EntityAvatar {...memberAvatar(member)} size="sm" />
 */
export function EntityAvatar({
	name,
	type = "human",
	avatarUrl,
	agent,
	size = "default",
	shape = "circle",
	fallback,
	badge,
	className,
}: EntityAvatarProps) {
	const src = resolveAvatarSrc(type, avatarUrl, agent);
	const radius = RADII[shape];

	return (
		<Avatar
			className={cn(
				SIZES[size],
				// Explicit radius classes beat the primitive's rounded-full, so
				// the image, fallback and ring all follow `shape`.
				radius.self,
				radius.ring,
				className,
			)}
		>
			{/* Always mounted, even without a src: base-ui's Avatar tracks
			    image-load status internally and only shows Fallback once it
			    observes a missing/failed src, so omitting this element when an
			    avatar is removed would leave it blank until a full reload. */}
			<AvatarImage
				src={src}
				alt=""
				className={cn(
					radius.self,
					// Provider logos are brand marks, not photos: keep them whole.
					!avatarUrl && src && "object-contain p-[15%]",
				)}
			/>
			<AvatarFallback
				className={cn(
					radius.self,
					FALLBACK_TEXT[size],
					"bg-primary/10 font-semibold leading-none text-primary",
					type !== "human" && "p-[15%]",
				)}
			>
				{type === "human" ? (
					getInitials(name) || fallback || <User className="size-1/2" />
				) : (
					<PacaMark />
				)}
			</AvatarFallback>
			{badge}
		</Avatar>
	);
}

/** The Paca mark, swapped for the light/dark theme with CSS alone. */
function PacaMark() {
	return (
		<>
			<img
				src={PACA_AVATAR_LIGHT}
				alt=""
				className="size-full object-contain dark:hidden"
			/>
			<img
				src={PACA_AVATAR_DARK}
				alt=""
				className="hidden size-full object-contain dark:block"
			/>
		</>
	);
}
