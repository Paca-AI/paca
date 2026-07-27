import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { ExternalLink, Sparkles } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { type ReleaseEntry, releasesQueryOptions } from "@/lib/changelog-api";

export const Route = createFileRoute("/_authenticated/admin/changelog/")({
	component: ChangelogPage,
});

// ── Minimal, safe markdown rendering for GitHub release notes ────────────────
// Handles the common cases (headings, bullet lists, **bold**, `code`, links)
// without pulling in a markdown dependency or using dangerouslySetInnerHTML.

// Only http(s)/mailto links are allowed in release notes; anything else
// (javascript:, data:, …) is rendered as plain text to avoid XSS.
function safeHref(url: string): string | undefined {
	try {
		const u = new URL(url, window.location.origin);
		if (
			u.protocol === "http:" ||
			u.protocol === "https:" ||
			u.protocol === "mailto:"
		) {
			return url;
		}
	} catch {
		// invalid URL → not rendered as a link
	}
	return undefined;
}

function renderInline(text: string): ReactNode[] {
	const nodes: ReactNode[] = [];
	const regex = /(\*\*([^*]+)\*\*|`([^`]+)`|\[([^\]]+)\]\(([^)]+)\))/g;
	let last = 0;
	let key = 0;
	let m: RegExpExecArray | null = regex.exec(text);
	while (m !== null) {
		if (m.index > last) nodes.push(text.slice(last, m.index));
		if (m[2] !== undefined) {
			nodes.push(<strong key={key++}>{m[2]}</strong>);
		} else if (m[3] !== undefined) {
			nodes.push(
				<code
					key={key++}
					className="rounded bg-muted px-1 py-0.5 text-xs font-mono"
				>
					{m[3]}
				</code>,
			);
		} else if (m[4] !== undefined) {
			const href = safeHref(m[5] ?? "");
			if (href) {
				nodes.push(
					<a
						key={key++}
						href={href}
						target="_blank"
						rel="noopener noreferrer"
						className="text-primary hover:underline"
					>
						{m[4]}
					</a>,
				);
			} else {
				nodes.push(m[4]);
			}
		}
		last = m.index + m[0].length;
		m = regex.exec(text);
	}
	if (last < text.length) nodes.push(text.slice(last));
	return nodes;
}

function renderNotes(body: string): ReactNode {
	const lines = body.replace(/\r\n/g, "\n").split("\n");
	const blocks: ReactNode[] = [];
	let bullets: ReactNode[] = [];

	const flush = () => {
		if (bullets.length) {
			blocks.push(
				<ul
					key={`ul-${blocks.length}`}
					className="list-disc space-y-1 pl-5 text-sm text-muted-foreground"
				>
					{bullets}
				</ul>,
			);
			bullets = [];
		}
	};

	lines.forEach((line) => {
		const t = line.trim();
		if (!t) {
			flush();
			return;
		}
		if (/^#{1,6}\s/.test(t)) {
			flush();
			blocks.push(
				<h3
					key={`h-${blocks.length}`}
					className="mt-3 text-sm font-semibold text-foreground"
				>
					{renderInline(t.replace(/^#{1,6}\s/, ""))}
				</h3>,
			);
			return;
		}
		if (/^[-*]\s/.test(t)) {
			bullets.push(
				<li key={`li-${blocks.length}-${bullets.length}`}>
					{renderInline(t.replace(/^[-*]\s/, ""))}
				</li>,
			);
			return;
		}
		flush();
		blocks.push(
			<p
				key={`p-${blocks.length}`}
				className="text-sm leading-relaxed text-muted-foreground"
			>
				{renderInline(t)}
			</p>,
		);
	});
	flush();
	return <div className="space-y-1.5">{blocks}</div>;
}

function formatDate(iso: string, locale: string): string {
	if (!iso) return "";
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return "";
	return d.toLocaleDateString(locale, {
		year: "numeric",
		month: "short",
		day: "numeric",
	});
}

function ReleaseCard({
	release,
	currentLabel,
}: {
	release: ReleaseEntry;
	currentLabel: string;
}) {
	const { i18n } = useTranslation();
	return (
		<article className="relative rounded-xl border border-border bg-card p-5">
			{release.isCurrent ? (
				<span className="absolute inset-x-0 top-0 h-px bg-primary/50" />
			) : null}
			<div className="mb-3 flex flex-wrap items-center gap-2">
				<h2 className="text-base font-semibold text-foreground">
					{release.name}
				</h2>
				{release.isCurrent ? (
					<Badge className="bg-primary text-primary-foreground">
						{currentLabel}
					</Badge>
				) : null}
				<span className="ml-auto text-xs text-muted-foreground">
					{formatDate(release.publishedAt, i18n.language)}
				</span>
				{release.url ? (
					<a
						href={release.url}
						target="_blank"
						rel="noopener noreferrer"
						className="text-muted-foreground transition-colors hover:text-foreground"
						aria-label={release.tag}
					>
						<ExternalLink className="size-3.5" />
					</a>
				) : null}
			</div>
			{release.body ? (
				renderNotes(release.body)
			) : (
				<p className="text-sm text-muted-foreground">—</p>
			)}
		</article>
	);
}

function ChangelogPage() {
	const { t } = useTranslation("admin");
	const { data, isLoading, isError } = useQuery(releasesQueryOptions());

	return (
		<div className="mx-auto flex w-full max-w-3xl flex-col gap-6 p-6">
			<header className="flex items-start gap-3">
				<div className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 ring-1 ring-primary/20">
					<Sparkles className="size-4 text-primary" />
				</div>
				<div>
					<h1 className="text-xl font-bold text-foreground">
						{t("changelog.title")}
					</h1>
					<p className="mt-0.5 text-sm text-muted-foreground">
						{t("changelog.description")}
					</p>
				</div>
			</header>

			{isLoading ? (
				<div className="space-y-4">
					{[0, 1, 2].map((i) => (
						<Skeleton key={i} className="h-32 w-full rounded-xl" />
					))}
				</div>
			) : isError ? (
				<div className="rounded-xl border border-border bg-card p-6 text-sm text-muted-foreground">
					{t("changelog.error")}
				</div>
			) : !data || data.releases.length === 0 ? (
				<div className="rounded-xl border border-border bg-card p-6 text-sm text-muted-foreground">
					{t("changelog.empty")}
				</div>
			) : (
				<div className="space-y-4">
					{data.releases.map((release) => (
						<ReleaseCard
							key={release.tag}
							release={release}
							currentLabel={t("changelog.current")}
						/>
					))}
				</div>
			)}
		</div>
	);
}
