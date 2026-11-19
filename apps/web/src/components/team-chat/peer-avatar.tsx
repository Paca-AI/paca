import { Bot, KeyRound } from "lucide-react";
import { EntityAvatar } from "@/components/shared/entity-avatar";
import type { AccountKind } from "@/lib/chat-api";
import { cn } from "@/lib/utils";

/**
 * Who a direct message is with: initials for a person, a robot for an agent, a
 * key for a service account, so the kind is clear at a glance and in the
 * sidebar without a label.
 */
export function PeerAvatar({
	name,
	kind,
	size = "sm",
	className,
}: {
	name: string;
	kind: AccountKind;
	size?: "xs" | "sm" | "default";
	className?: string;
}) {
	if (kind === "user") {
		return <EntityAvatar name={name} size={size} className={className} />;
	}
	const box = size === "xs" ? "size-5" : size === "sm" ? "size-6" : "size-8";
	const Icon = kind === "agent" ? Bot : KeyRound;
	return (
		<span
			aria-hidden
			className={cn(
				"flex shrink-0 items-center justify-center rounded-full",
				kind === "agent"
					? "bg-violet-500/15 text-violet-600 dark:text-violet-300"
					: "bg-amber-500/15 text-amber-700 dark:text-amber-300",
				box,
				className,
			)}
		>
			<Icon className="size-3.5" />
		</span>
	);
}
