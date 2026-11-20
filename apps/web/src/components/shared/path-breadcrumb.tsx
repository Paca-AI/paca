import { type ReactNode, useState } from "react";
import {
	Breadcrumb,
	BreadcrumbEllipsis,
	BreadcrumbItem,
	BreadcrumbList,
	BreadcrumbPage,
	BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

/** One step of a location path. */
export interface PathCrumb {
	key: string;
	label: string;
	/** Opens this location. Without it the crumb is plain text. */
	onSelect?: () => void;
	/**
	 * Makes the crumb a drop target for dragged items (the file manager's
	 * move-by-drag). `mime` is the drag payload type, `onDrop` gets its parsed
	 * ids.
	 */
	drop?: { mime: string; onDrop: (ids: string[]) => void };
}

/** Crumbs kept visible before the middle ones fold into a menu. */
const MAX_VISIBLE = 4;

/** Drag-over handling and highlight for a crumb that accepts drops. */
function useCrumbDrop(drop: PathCrumb["drop"]) {
	const [over, setOver] = useState(false);
	if (!drop) return { over: false, props: {} };
	return {
		over,
		props: {
			onDragOver: (e: React.DragEvent) => {
				if (e.dataTransfer.types.includes(drop.mime)) {
					e.preventDefault();
					setOver(true);
				}
			},
			onDragLeave: () => setOver(false),
			onDrop: (e: React.DragEvent) => {
				setOver(false);
				const raw = e.dataTransfer.getData(drop.mime);
				if (raw) {
					e.preventDefault();
					drop.onDrop(JSON.parse(raw) as string[]);
				}
			},
		},
	};
}

const DROP_HIGHLIGHT = "bg-primary/20 ring-2 ring-primary";

function CrumbButton({ crumb }: { crumb: PathCrumb }) {
	const { over, props } = useCrumbDrop(crumb.drop);
	const className = cn(
		"max-w-32 truncate rounded px-1 transition-colors",
		crumb.onSelect && "hover:bg-muted hover:text-foreground",
		over && DROP_HIGHLIGHT,
	);
	if (!crumb.onSelect) {
		return (
			<span className={className} {...props}>
				{crumb.label}
			</span>
		);
	}
	return (
		<button
			type="button"
			className={className}
			onClick={crumb.onSelect}
			{...props}
		>
			{crumb.label}
		</button>
	);
}

function CurrentCrumb({ crumb }: { crumb: PathCrumb }) {
	const { over, props } = useCrumbDrop(crumb.drop);
	return (
		<BreadcrumbPage
			className={cn(
				"max-w-60 truncate rounded px-1 font-medium",
				over && DROP_HIGHLIGHT,
			)}
			{...props}
		>
			{crumb.label}
		</BreadcrumbPage>
	);
}

/**
 * The location of a file, document or folder, in the look documentation has
 * always had, shared by the docs editor and the file manager: muted ancestors
 * that open their folder, the current location in bold, and (for the file
 * manager) drop targets so items can be moved by dragging them onto a crumb.
 * A long path keeps its first and last crumbs and folds the middle into a
 * menu. `loading` shows a placeholder for the current crumb.
 */
export function PathBreadcrumb({
	crumbs,
	loading,
	className,
}: {
	crumbs: PathCrumb[];
	/** The last crumb is not known yet. */
	loading?: boolean;
	className?: string;
}) {
	const last = crumbs.length - 1;
	const folded = crumbs.length > MAX_VISIBLE;
	// Keep the first crumb and the last two; the rest go in the menu.
	const hidden = folded ? crumbs.slice(1, last - 1) : [];
	const shown = folded
		? [
				{ crumb: crumbs[0], index: 0 },
				{ crumb: crumbs[last - 1], index: last - 1 },
				{ crumb: crumbs[last], index: last },
			]
		: crumbs.map((crumb, index) => ({ crumb, index }));

	return (
		<Breadcrumb className={cn("min-w-0", className)}>
			<BreadcrumbList className="flex-nowrap gap-1 text-xs sm:gap-1">
				{shown.map(({ crumb, index }) => (
					<CrumbWithSeparator
						key={crumb.key}
						separator={index > 0}
						ellipsis={folded && index === last - 1 ? hidden : null}
					>
						<BreadcrumbItem className="min-w-0">
							{index === last ? (
								loading ? (
									<Skeleton className="h-3 w-28" />
								) : (
									<CurrentCrumb crumb={crumb} />
								)
							) : (
								<CrumbButton crumb={crumb} />
							)}
						</BreadcrumbItem>
					</CrumbWithSeparator>
				))}
				{loading && crumbs.length === 0 && (
					<BreadcrumbItem className="min-w-0">
						<Skeleton className="h-3 w-28" />
					</BreadcrumbItem>
				)}
			</BreadcrumbList>
		</Breadcrumb>
	);
}

/** A crumb with its leading separator, and (when folded) the hidden-crumbs menu before it. */
function CrumbWithSeparator({
	separator,
	ellipsis,
	children,
}: {
	separator: boolean;
	ellipsis: PathCrumb[] | null;
	children: ReactNode;
}) {
	return (
		<>
			{ellipsis && ellipsis.length > 0 && (
				<>
					<BreadcrumbSeparator />
					<BreadcrumbItem>
						<DropdownMenu>
							<DropdownMenuTrigger
								className="flex size-5 items-center justify-center rounded hover:bg-muted"
								aria-label="…"
							>
								<BreadcrumbEllipsis />
							</DropdownMenuTrigger>
							<DropdownMenuContent align="start">
								{ellipsis.map((c) => (
									<DropdownMenuItem
										key={c.key}
										disabled={!c.onSelect}
										onClick={c.onSelect}
									>
										{c.label}
									</DropdownMenuItem>
								))}
							</DropdownMenuContent>
						</DropdownMenu>
					</BreadcrumbItem>
				</>
			)}
			{separator && <BreadcrumbSeparator />}
			{children}
		</>
	);
}
