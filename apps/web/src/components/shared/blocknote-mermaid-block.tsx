import { createReactBlockSpec } from "@blocknote/react";
import { Code2, Pencil } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useThemeMode } from "@/hooks/use-theme-mode";

// mermaid is initialized lazily, exactly once, on first render of a diagram.
// securityLevel "strict" is the load-bearing XSS control: diagram source is
// user-authored and shared across a project, so mermaid must sanitize labels
// and refuse to run embedded scripts/click-handlers. startOnLoad is off
// because we drive rendering ourselves per block.
let mermaidInit: Promise<typeof import("mermaid").default> | null = null;
function getMermaid(dark: boolean) {
	if (!mermaidInit) {
		// Dynamic import keeps mermaid (>1MB) out of the main bundle.
		mermaidInit = import("mermaid").then((m) => {
			m.default.initialize({
				startOnLoad: false,
				securityLevel: "strict",
				theme: dark ? "dark" : "default",
				// Render each diagram at its natural pixel size rather than
				// mermaid's default "shrink to fit an inline max-width" behavior,
				// which collapses a diagram to a few pixels inside a flex/narrow
				// container. The wrapper scrolls horizontally when a diagram is
				// genuinely wider than the editor.
				er: { useMaxWidth: false },
				flowchart: { useMaxWidth: false },
				sequence: { useMaxWidth: false },
				class: { useMaxWidth: false },
				state: { useMaxWidth: false },
				gantt: { useMaxWidth: false },
				journey: { useMaxWidth: false },
				pie: { useMaxWidth: false },
			});
			return m.default;
		});
	}
	return mermaidInit;
}

// mermaid.render() needs a DOM-id-safe, unique id per diagram (it mounts a
// temporary element under that id); block.id can contain characters invalid
// in an id, so strip them.
function renderId(blockId: string): string {
	return `mermaid-${blockId.replace(/[^a-zA-Z0-9_-]/g, "")}`;
}

function MermaidDiagram({ code, blockId }: { code: string; blockId: string }) {
	const { resolvedMode } = useThemeMode();
	const [svg, setSvg] = useState<string>("");
	const [error, setError] = useState<string>("");

	useEffect(() => {
		let cancelled = false;
		const source = code.trim();
		if (!source) {
			setSvg("");
			setError("");
			return;
		}
		(async () => {
			try {
				const mermaid = await getMermaid(resolvedMode === "dark");
				// parse first so an invalid diagram surfaces a clean error
				// instead of mermaid injecting its own error graphic.
				await mermaid.parse(source);
				const { svg: out } = await mermaid.render(renderId(blockId), source);
				if (!cancelled) {
					setSvg(out);
					setError("");
				}
			} catch (e) {
				if (!cancelled) {
					setSvg("");
					setError(e instanceof Error ? e.message : String(e));
				}
			}
		})();
		return () => {
			cancelled = true;
		};
	}, [code, blockId, resolvedMode]);

	if (error) {
		return (
			<div className="rounded-md border border-destructive/40 bg-destructive/5 px-3 py-2 text-xs text-destructive whitespace-pre-wrap font-mono">
				Mermaid error: {error}
			</div>
		);
	}
	if (!svg) {
		return (
			<div className="text-xs text-muted-foreground italic px-1 py-2">
				Empty Mermaid diagram — add diagram source.
			</div>
		);
	}
	// Plain block (not flex — a flex item with mermaid's width can collapse to
	// min-content) that scrolls horizontally; the SVG keeps its natural size,
	// with a min-width so a tiny diagram still reads and h-auto to keep aspect.
	return (
		<div
			className="overflow-x-auto py-1 [&_svg]:h-auto [&_svg]:!max-w-none"
			// svg is produced by mermaid under securityLevel "strict" (labels
			// sanitized, no script/click handlers), so injecting it is safe.
			// biome-ignore lint/security/noDangerouslySetInnerHtml: sanitized mermaid SVG
			dangerouslySetInnerHTML={{ __html: svg }}
		/>
	);
}

export const MermaidBlock = createReactBlockSpec(
	{
		type: "mermaid",
		propSchema: {
			code: { default: "" },
		},
		content: "none",
	},
	{
		render: ({ block, editor }) => {
			const code = (block.props as { code: string }).code;
			return (
				<MermaidBlockView
					code={code}
					editable={editor.isEditable}
					onChange={(next) =>
						editor.updateBlock(block, { props: { code: next } })
					}
					blockId={block.id}
				/>
			);
		},
		// Degrade to a fenced ```mermaid code block for markdown/HTML export and
		// for any client (older or upstream-before-this-feature) that doesn't
		// know the mermaid block type — the source is never trapped.
		toExternalHTML: ({ block }) => {
			const code = (block.props as { code: string }).code;
			return (
				<pre>
					<code className="language-mermaid">{code}</code>
				</pre>
			);
		},
	},
);

function MermaidBlockView({
	code,
	editable,
	onChange,
	blockId,
}: {
	code: string;
	editable: boolean;
	onChange: (next: string) => void;
	blockId: string;
}) {
	// Open the source editor automatically for a brand-new (empty) diagram.
	const [editing, setEditing] = useState(() => editable && code.trim() === "");
	const taRef = useRef<HTMLTextAreaElement | null>(null);

	useEffect(() => {
		if (editing) taRef.current?.focus();
	}, [editing]);

	return (
		<div className="my-1 rounded-lg border border-border/40 bg-muted/20 p-2">
			<MermaidDiagram code={code} blockId={blockId} />
			{editable && (
				<>
					<div className="mt-1 flex justify-end">
						<button
							type="button"
							onClick={() => setEditing((v) => !v)}
							className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
						>
							{editing ? (
								<>
									<Code2 width={12} height={12} /> Hide source
								</>
							) : (
								<>
									<Pencil width={12} height={12} /> Edit
								</>
							)}
						</button>
					</div>
					{editing && (
						<textarea
							ref={taRef}
							value={code}
							onChange={(e) => onChange(e.target.value)}
							// Stop BlockNote from hijacking editing keystrokes
							// (Enter/Backspace/arrows) while typing diagram source.
							onKeyDown={(e) => e.stopPropagation()}
							spellCheck={false}
							rows={Math.max(3, code.split("\n").length + 1)}
							placeholder={"graph TD\n  A --> B"}
							className="mt-1 w-full resize-y rounded-md border border-border/40 bg-background px-2 py-1.5 font-mono text-xs leading-relaxed outline-none focus:border-border"
						/>
					)}
				</>
			)}
		</div>
	);
}
