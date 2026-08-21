import { useAui, useAuiState } from "@assistant-ui/react";
import { FilePenLine, ListChecks, SquareSlash } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { TooltipIconButton } from "@/components/assistant-ui/tooltip-icon-button";
import {
	isProjectChatCommandQuery,
	type ProjectChatCommandKind,
	projectChatCommandToken,
} from "@/lib/project-chat-commands";
import { cn } from "@/lib/utils";

interface CommandItem {
	kind: ProjectChatCommandKind;
	token: string;
	label: string;
	icon: typeof FilePenLine;
}

export function ProjectChatCommandMenu({
	disabled,
	hasTaskContext,
}: {
	disabled?: boolean;
	hasTaskContext: boolean;
}) {
	const { t } = useTranslation("projects");
	const aui = useAui();
	const composerText = useAuiState((state) => state.composer.text);
	const rootRef = useRef<HTMLDivElement>(null);
	const [iconOpen, setIconOpen] = useState(false);
	const [dismissedQuery, setDismissedQuery] = useState<string>();
	const [activeIndex, setActiveIndex] = useState(0);
	const menuLabel = t("chats.conclusion.commandMenu");
	const items = useMemo<CommandItem[]>(
		() => [
			{
				kind: "update-description",
				token: projectChatCommandToken("update-description"),
				label: t("chats.conclusion.modeDescription"),
				icon: FilePenLine,
			},
			{
				kind: "record-conclusion",
				token: projectChatCommandToken("record-conclusion"),
				label: t("chats.conclusion.modeSummary"),
				icon: ListChecks,
			},
		],
		[t],
	);
	const query = composerText.startsWith("/")
		? composerText.slice(1).toLocaleLowerCase()
		: "";
	const filteredItems = items.filter(
		(item) =>
			item.token.slice(1).toLocaleLowerCase().includes(query) ||
			item.label.toLocaleLowerCase().includes(query),
	);
	const typedOpen =
		!disabled &&
		isProjectChatCommandQuery(composerText) &&
		dismissedQuery !== composerText;
	const open = !disabled && (iconOpen || typedOpen);

	useEffect(() => {
		setActiveIndex(0);
		if (dismissedQuery && dismissedQuery !== composerText) {
			setDismissedQuery(undefined);
		}
	}, [composerText, dismissedQuery]);

	useEffect(() => {
		if (!iconOpen) return;
		const closeOnOutsidePress = (event: PointerEvent) => {
			if (!rootRef.current?.contains(event.target as Node)) setIconOpen(false);
		};
		document.addEventListener("pointerdown", closeOnOutsidePress);
		return () =>
			document.removeEventListener("pointerdown", closeOnOutsidePress);
	}, [iconOpen]);

	const choose = useCallback(
		(item: CommandItem) => {
			if (!hasTaskContext) return;
			aui.composer().setText(`${item.token} `);
			setIconOpen(false);
			setDismissedQuery(undefined);
		},
		[aui, hasTaskContext],
	);

	useEffect(() => {
		if (!open) return;
		const handleKeyDown = (event: KeyboardEvent) => {
			const target = event.target as HTMLElement | null;
			if (!target?.classList.contains("aui-composer-input")) return;
			if (event.key === "Escape") {
				event.preventDefault();
				setIconOpen(false);
				if (typedOpen) setDismissedQuery(composerText);
				return;
			}
			if (filteredItems.length === 0) return;
			if (event.key === "ArrowDown" || event.key === "ArrowUp") {
				event.preventDefault();
				setActiveIndex((current) => {
					const delta = event.key === "ArrowDown" ? 1 : -1;
					return (
						(current + delta + filteredItems.length) % filteredItems.length
					);
				});
				return;
			}
			if (event.key === "Enter" && hasTaskContext) {
				event.preventDefault();
				const item = filteredItems[activeIndex];
				if (item) choose(item);
			}
		};
		document.addEventListener("keydown", handleKeyDown, true);
		return () => document.removeEventListener("keydown", handleKeyDown, true);
	}, [
		activeIndex,
		composerText,
		filteredItems,
		hasTaskContext,
		open,
		typedOpen,
		choose,
	]);

	return (
		<div ref={rootRef} className="relative">
			<TooltipIconButton
				type="button"
				variant="ghost"
				size="icon"
				className="size-7 rounded-full"
				tooltip={menuLabel}
				aria-label={menuLabel}
				disabled={disabled}
				onClick={() => setIconOpen((value) => !value)}
			>
				<SquareSlash className="size-4" />
			</TooltipIconButton>
			{open && (
				<div
					role="listbox"
					aria-label={menuLabel}
					className="absolute bottom-9 left-0 z-50 w-64 rounded-lg bg-popover p-1 text-popover-foreground shadow-lg ring-1 ring-foreground/10"
				>
					{filteredItems.map((item, index) => {
						const Icon = item.icon;
						return (
							<button
								type="button"
								role="option"
								aria-selected={index === activeIndex}
								key={item.kind}
								disabled={!hasTaskContext}
								onMouseDown={(event) => event.preventDefault()}
								onMouseEnter={() => setActiveIndex(index)}
								onClick={() => choose(item)}
								className={cn(
									"flex w-full items-center gap-2 rounded-md px-2 py-2 text-left outline-none disabled:cursor-not-allowed disabled:opacity-45",
									index === activeIndex && "bg-accent text-accent-foreground",
								)}
							>
								<Icon className="size-4 shrink-0 text-primary" />
								<span className="min-w-0 flex-1 truncate text-sm font-medium">
									{item.label}
								</span>
							</button>
						);
					})}
					{!hasTaskContext && (
						<p className="border-t border-border/50 px-2 py-1.5 text-xs text-muted-foreground">
							{t("chats.context.add")}
						</p>
					)}
				</div>
			)}
		</div>
	);
}
