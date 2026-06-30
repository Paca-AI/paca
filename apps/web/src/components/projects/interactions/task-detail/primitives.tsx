// Shared UI primitives used across task-detail sub-components

import { useTranslation } from "react-i18next";

export function FieldRow({
	label,
	children,
}: {
	label: string;
	children: React.ReactNode;
}) {
	return (
		<div className="grid grid-cols-1 md:grid-cols-[9.5rem_1fr] items-start md:items-center gap-1.5 md:gap-4 py-3 px-2.5 group/field rounded-lg hover:bg-muted/40 transition-colors duration-200">
			<span className="text-sm font-medium text-muted-foreground leading-snug select-none">
				{label}
			</span>
			<div className="min-w-0">{children}</div>
		</div>
	);
}

export function FieldValue({
	children,
	empty,
}: {
	children?: React.ReactNode;
	empty?: boolean;
}) {
	const { t } = useTranslation();
	if (empty) {
		return (
			<span className="text-sm text-muted-foreground/50 italic">
				{t("project.interactions.taskDetail.empty")}
			</span>
		);
	}
	return (
		<span className="text-sm font-medium text-foreground">{children}</span>
	);
}

export function SectionHeading({ children }: { children: React.ReactNode }) {
	return (
		<h3 className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/70 mb-4 flex items-center gap-2">
			<span>{children}</span>
			<div className="flex-1 h-px bg-linear-to-r from-border/40 to-transparent" />
		</h3>
	);
}
