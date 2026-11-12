"use client";

import { CheckIcon, CopyIcon } from "lucide-react";
import {
	type ComponentProps,
	isValidElement,
	memo,
	type ReactNode,
	useState,
} from "react";
import { useTranslation } from "react-i18next";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";

import { Button } from "@/components/ui/button";
import {
	CodeBlock,
	CodeBlockActions,
	CodeBlockContent,
	CodeBlockHeader,
	CodeBlockTitle,
} from "@/components/ui/code-block";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { copyToClipboard } from "@/lib/clipboard";
import { cn } from "@/lib/utils";

const REMARK_PLUGINS = [remarkGfm];

type MarkdownTextProps = { text: string; className?: string };

/** Renders (possibly still streaming) markdown text. */
export const MarkdownText = memo(function MarkdownText({
	text,
	className,
}: MarkdownTextProps) {
	return (
		<div className={cn("aui-md", className)}>
			<ReactMarkdown remarkPlugins={REMARK_PLUGINS} components={components}>
				{text}
			</ReactMarkdown>
		</div>
	);
});

/** Plain text of a React subtree (the code string inside a fenced block). */
function textOf(node: ReactNode): string {
	if (typeof node === "string" || typeof node === "number") return String(node);
	if (Array.isArray(node)) return node.map(textOf).join("");
	if (isValidElement<{ children?: ReactNode }>(node)) {
		return textOf(node.props.children);
	}
	return "";
}

function CopyCodeButton({ code }: { code: string }) {
	const { t } = useTranslation("projects");
	const [copied, setCopied] = useState(false);
	return (
		<Button
			type="button"
			variant="ghost"
			size="icon-xs"
			aria-label={t("agents.thread.copy")}
			onClick={() => {
				if (!code || copied) return;
				void copyToClipboard(code).then((ok) => {
					if (!ok) return;
					setCopied(true);
					setTimeout(() => setCopied(false), 3000);
				});
			}}
		>
			{copied ? <CheckIcon /> : <CopyIcon />}
		</Button>
	);
}

/** A fenced block: shadcn CodeBlock with the language and a copy button. */
function Pre({ children }: ComponentProps<"pre">) {
	const code = textOf(children).replace(/\n$/, "");
	const className = isValidElement<{ className?: string }>(children)
		? children.props.className
		: undefined;
	const language = /language-([\w-]+)/.exec(className ?? "")?.[1];
	return (
		<CodeBlock size="sm" className="my-3">
			{language ? (
				<CodeBlockHeader>
					<CodeBlockTitle className="font-mono lowercase text-muted-foreground">
						{language}
					</CodeBlockTitle>
					<CodeBlockActions>
						<CopyCodeButton code={code} />
					</CodeBlockActions>
				</CodeBlockHeader>
			) : (
				<CodeBlockActions>
					<CopyCodeButton code={code} />
				</CodeBlockActions>
			)}
			<CodeBlockContent>{code}</CodeBlockContent>
		</CodeBlock>
	);
}

const components: Components = {
	h1: ({ className, node: _n, ...props }) => (
		<h1
			className={cn(
				"mt-5 mb-2 scroll-m-20 text-xl font-semibold first:mt-0 last:mb-0",
				className,
			)}
			{...props}
		/>
	),
	h2: ({ className, node: _n, ...props }) => (
		<h2
			className={cn(
				"mt-5 mb-2 scroll-m-20 text-lg font-semibold first:mt-0 last:mb-0",
				className,
			)}
			{...props}
		/>
	),
	h3: ({ className, node: _n, ...props }) => (
		<h3
			className={cn(
				"mt-4 mb-1.5 scroll-m-20 text-base font-semibold first:mt-0 last:mb-0",
				className,
			)}
			{...props}
		/>
	),
	h4: ({ className, node: _n, ...props }) => (
		<h4
			className={cn(
				"mt-3.5 mb-1 scroll-m-20 text-base font-medium first:mt-0 last:mb-0",
				className,
			)}
			{...props}
		/>
	),
	h5: ({ className, node: _n, ...props }) => (
		<h5
			className={cn(
				"mt-3 mb-1 text-sm font-semibold first:mt-0 last:mb-0",
				className,
			)}
			{...props}
		/>
	),
	h6: ({ className, node: _n, ...props }) => (
		<h6
			className={cn(
				"mt-3 mb-1 text-sm font-medium first:mt-0 last:mb-0",
				className,
			)}
			{...props}
		/>
	),
	p: ({ className, node: _n, ...props }) => (
		<p
			className={cn("my-3 leading-relaxed first:mt-0 last:mb-0", className)}
			{...props}
		/>
	),
	a: ({ className, node: _n, ...props }) => (
		<a
			className={cn(
				"text-primary underline underline-offset-2 hover:text-primary/80",
				className,
			)}
			target="_blank"
			rel="noreferrer noopener"
			{...props}
		/>
	),
	blockquote: ({ className, node: _n, ...props }) => (
		<blockquote
			className={cn(
				"my-3 border-s-2 border-muted-foreground/30 ps-4 text-muted-foreground",
				className,
			)}
			{...props}
		/>
	),
	ul: ({ className, node: _n, ...props }) => (
		<ul
			className={cn(
				"my-3 ms-5 list-disc marker:text-muted-foreground [&>li]:mt-1",
				className,
			)}
			{...props}
		/>
	),
	ol: ({ className, node: _n, ...props }) => (
		<ol
			className={cn(
				"my-3 ms-5 list-decimal marker:text-muted-foreground [&>li]:mt-1",
				className,
			)}
			{...props}
		/>
	),
	hr: ({ className, node: _n, ...props }) => (
		<hr
			className={cn("my-3 border-muted-foreground/20", className)}
			{...props}
		/>
	),
	table: ({ node: _n, ...props }) => <Table className="my-3" {...props} />,
	thead: ({ node: _n, ...props }) => <TableHeader {...props} />,
	tbody: ({ node: _n, ...props }) => <TableBody {...props} />,
	tr: ({ node: _n, ...props }) => <TableRow {...props} />,
	th: ({ node: _n, ...props }) => <TableHead {...props} />,
	td: ({ node: _n, ...props }) => <TableCell {...props} />,
	li: ({ className, node: _n, ...props }) => (
		<li className={cn("leading-relaxed", className)} {...props} />
	),
	strong: ({ className, node: _n, ...props }) => (
		<strong className={cn("font-semibold", className)} {...props} />
	),
	sup: ({ className, node: _n, ...props }) => (
		<sup
			className={cn("[&>a]:text-xs [&>a]:no-underline", className)}
			{...props}
		/>
	),
	pre: ({ node: _n, ...props }) => <Pre {...props} />,
	code: ({ className, node: _n, ...props }) => (
		<code
			className={cn(
				// Fenced blocks are styled by <Pre>; only inline code gets a chip.
				!/language-/.test(className ?? "") &&
					"rounded-md bg-muted px-1.5 py-0.5 font-mono text-[0.85em]",
				className,
			)}
			{...props}
		/>
	),
};
