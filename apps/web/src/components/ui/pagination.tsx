import { ChevronLeft, ChevronRight } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export interface PaginationProps {
	page: number;
	totalPages: number;
	onPageChange: (page: number) => void;
	className?: string;
}

type PageItem = number | "ellipsis-start" | "ellipsis-end";

/** Collapses a long page range around the current page, always keeping the
 *  first and last page reachable in one click — e.g. [1, …, 4, 5, 6, …, 20]. */
function buildPageItems(page: number, totalPages: number): PageItem[] {
	if (totalPages <= 7) {
		return Array.from({ length: totalPages }, (_, i) => i + 1);
	}
	const items: PageItem[] = [1];
	const start = Math.max(2, page - 2);
	const end = Math.min(totalPages - 1, page + 2);
	if (start > 2) items.push("ellipsis-start");
	for (let p = start; p <= end; p++) items.push(p);
	if (end < totalPages - 1) items.push("ellipsis-end");
	items.push(totalPages);
	return items;
}

/** Page-number pagination for lists where the backend already returns
 *  `{ total, page, page_size }` — jumping straight to a page (or seeing
 *  "page 3 of 9") fits structured, row-actionable data better than infinite
 *  scroll. Renders nothing for a single page. */
export function Pagination({
	page,
	totalPages,
	onPageChange,
	className,
}: PaginationProps) {
	const { t } = useTranslation("common");

	if (totalPages <= 1) return null;

	const items = buildPageItems(page, totalPages);

	return (
		<nav
			aria-label={t("pagination.navLabel")}
			className={cn(
				"flex flex-wrap items-center justify-between gap-3",
				className,
			)}
		>
			<p className="text-sm text-muted-foreground">
				{t("pagination.pageOfTotal", { page, totalPages })}
			</p>
			<div className="flex items-center gap-1">
				<Button
					type="button"
					variant="outline"
					size="icon-sm"
					disabled={page <= 1}
					onClick={() => onPageChange(page - 1)}
					aria-label={t("pagination.previous")}
				>
					<ChevronLeft className="size-3.5" />
				</Button>
				{items.map((item) =>
					typeof item === "number" ? (
						<Button
							key={item}
							type="button"
							variant={item === page ? "secondary" : "ghost"}
							size="icon-sm"
							onClick={() => onPageChange(item)}
							aria-label={t("pagination.page", { page: item })}
							aria-current={item === page ? "page" : undefined}
						>
							{item}
						</Button>
					) : (
						<span
							key={item}
							aria-hidden="true"
							className="px-1 text-sm text-muted-foreground"
						>
							…
						</span>
					),
				)}
				<Button
					type="button"
					variant="outline"
					size="icon-sm"
					disabled={page >= totalPages}
					onClick={() => onPageChange(page + 1)}
					aria-label={t("pagination.next")}
				>
					<ChevronRight className="size-3.5" />
				</Button>
			</div>
		</nav>
	);
}
