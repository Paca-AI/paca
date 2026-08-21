import {
	createContext,
	type HTMLAttributes,
	type ReactNode,
	useContext,
	useId,
	useState,
} from "react";
import { cn } from "@/lib/utils";

type TabsState = {
	value: string;
	setValue: (value: string) => void;
	baseId: string;
};
const TabsContext = createContext<TabsState | null>(null);

function useTabs() {
	const value = useContext(TabsContext);
	if (!value) throw new Error("Tabs components must be rendered inside Tabs");
	return value;
}

export function Tabs({
	defaultValue,
	value: controlledValue,
	onValueChange,
	className,
	children,
}: {
	defaultValue: string;
	value?: string;
	onValueChange?: (value: string) => void;
	className?: string;
	children: ReactNode;
}) {
	const [internalValue, setInternalValue] = useState(defaultValue);
	const value = controlledValue ?? internalValue;
	const setValue = (nextValue: string) => {
		if (controlledValue === undefined) setInternalValue(nextValue);
		onValueChange?.(nextValue);
	};
	const baseId = useId();
	return (
		<TabsContext.Provider value={{ value, setValue, baseId }}>
			<div className={className}>{children}</div>
		</TabsContext.Provider>
	);
}

export function TabsList({
	className,
	...props
}: HTMLAttributes<HTMLDivElement>) {
	return (
		<div
			role="tablist"
			className={cn("rounded-lg bg-muted p-1", className)}
			{...props}
		/>
	);
}

export function TabsTrigger({
	value,
	className,
	children,
	disabled = false,
}: {
	value: string;
	className?: string;
	children: ReactNode;
	disabled?: boolean;
}) {
	const tabs = useTabs();
	const active = tabs.value === value;
	const suffix = value.replace(/[^a-zA-Z0-9_-]/g, "-");
	const tabId = `${tabs.baseId}-tab-${suffix}`;
	const panelId = `${tabs.baseId}-panel-${suffix}`;
	return (
		<button
			type="button"
			role="tab"
			id={tabId}
			aria-controls={panelId}
			aria-selected={active}
			disabled={disabled}
			tabIndex={active ? 0 : -1}
			onClick={() => tabs.setValue(value)}
			onKeyDown={(event) => {
				if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key))
					return;
				const triggers = Array.from(
					event.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>(
						'[role="tab"]:not(:disabled)',
					) ?? [],
				);
				if (triggers.length === 0) return;
				event.preventDefault();
				const currentIndex = triggers.indexOf(event.currentTarget);
				const nextIndex =
					event.key === "Home"
						? 0
						: event.key === "End"
							? triggers.length - 1
							: event.key === "ArrowRight"
								? (currentIndex + 1) % triggers.length
								: (currentIndex - 1 + triggers.length) % triggers.length;
				const next = triggers[nextIndex];
				next?.focus();
				next?.click();
			}}
			className={cn(
				"rounded-md px-3 py-1.5 text-sm transition-colors",
				active
					? "bg-background font-medium text-foreground shadow-sm"
					: "text-muted-foreground hover:text-foreground",
				className,
			)}
		>
			{children}
		</button>
	);
}

export function TabsContent({
	value,
	className,
	children,
}: {
	value: string;
	className?: string;
	children: ReactNode;
}) {
	const tabs = useTabs();
	if (tabs.value !== value) return null;
	const suffix = value.replace(/[^a-zA-Z0-9_-]/g, "-");
	return (
		<div
			role="tabpanel"
			id={`${tabs.baseId}-panel-${suffix}`}
			aria-labelledby={`${tabs.baseId}-tab-${suffix}`}
			className={className}
		>
			{children}
		</div>
	);
}
