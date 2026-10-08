import { useMutation } from "@tanstack/react-query";
import { CircleCheck, CircleX } from "lucide-react";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import type { Policy } from "@/lib/policy";
import {
	type AttributeDef,
	type SimulateResult,
	simulatePolicy,
} from "@/lib/role-api";

type AttributeValue = string | boolean | string[];

/**
 * Parses "key=value" lines. A value is a string; a comma makes it a list; for
 * an attribute the schema types as bool, "true"/"false" is a boolean. Returns
 * the first bad line (no "=") as `error`.
 */
export function parseAttributes(
	text: string,
	schema: readonly AttributeDef[],
): { attributes: Record<string, AttributeValue>; error: string | null } {
	const attributes: Record<string, AttributeValue> = {};
	for (const raw of text.split("\n")) {
		const line = raw.trim();
		if (!line) continue;
		const eq = line.indexOf("=");
		const key = eq === -1 ? "" : line.slice(0, eq).trim();
		if (!key) return { attributes, error: line };
		const value = line.slice(eq + 1).trim();
		const def = schema.find((d) => d.key === key);
		if (def?.type === "bool") {
			attributes[key] = value === "true";
		} else if (value.includes(",")) {
			attributes[key] = value
				.split(",")
				.map((v) => v.trim())
				.filter(Boolean);
		} else {
			attributes[key] = value;
		}
	}
	return { attributes, error: null };
}

interface SimulatePanelProps {
	/** The policy JSON being edited. */
	json: string;
	/** The project the policy is evaluated in; omitted for a platform role. */
	projectId?: string;
	actions: readonly string[];
	schema: readonly AttributeDef[];
}

function parsePolicy(json: string): Policy | null {
	try {
		const v = JSON.parse(json) as unknown;
		if (v === null || typeof v !== "object" || Array.isArray(v)) return null;
		return Array.isArray((v as Policy).statements) ? (v as Policy) : null;
	} catch {
		return null;
	}
}

/** The statement that decided the request: the first matching Deny when it
 *  was refused by one, the first matching Allow when it was allowed. */
function deciding(result: SimulateResult) {
	return result.matched.find((m) =>
		result.allowed ? m.effect === "Allow" : m.effect === "Deny",
	);
}

/**
 * Asks the server "would this request be allowed?" for the policy in the
 * editor: an action, a resource and optional attribute values, answered with
 * allow or deny and the statement that decided it.
 */
export function SimulatePanel({
	json,
	projectId,
	actions,
	schema,
}: SimulatePanelProps) {
	const { t } = useTranslation("roles");
	const id = useId();
	const [action, setAction] = useState("");
	const [resource, setResource] = useState("");
	const [attrText, setAttrText] = useState("");
	const [attrError, setAttrError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: (input: Parameters<typeof simulatePolicy>[0]) =>
			simulatePolicy(input, projectId),
	});

	const policy = parsePolicy(json);
	const ready = !!policy && action.trim() !== "" && resource.trim() !== "";

	const run = () => {
		if (!policy) return;
		const parsed = parseAttributes(attrText, schema);
		if (parsed.error !== null) {
			setAttrError(parsed.error);
			return;
		}
		setAttrError(null);
		const hasAttrs = Object.keys(parsed.attributes).length > 0;
		mutation.mutate({
			policy,
			action: action.trim(),
			resource: resource.trim(),
			...(hasAttrs ? { attributes: parsed.attributes } : {}),
		});
	};

	const result = mutation.data;
	const decider = result ? deciding(result) : undefined;

	return (
		<section
			aria-label={t("simulate.title")}
			className="flex flex-col gap-3 rounded-lg border bg-muted/20 p-3"
		>
			<div>
				<h4 className="text-xs font-semibold text-muted-foreground">
					{t("simulate.title")}
				</h4>
				<p className="text-xs text-muted-foreground">{t("simulate.hint")}</p>
			</div>
			<div className="grid gap-3 sm:grid-cols-2">
				<div className="flex flex-col gap-1.5">
					<Label htmlFor={`${id}-action`} className="text-xs">
						{t("simulate.action")}
					</Label>
					<Input
						id={`${id}-action`}
						list={`${id}-actions`}
						value={action}
						onChange={(e) => setAction(e.target.value)}
						placeholder="tasks:write"
						autoComplete="off"
						spellCheck={false}
						className="font-mono text-xs"
					/>
					<datalist id={`${id}-actions`}>
						{actions.map((a) => (
							<option key={a} value={a} />
						))}
					</datalist>
				</div>
				<div className="flex flex-col gap-1.5">
					<Label htmlFor={`${id}-resource`} className="text-xs">
						{t("simulate.resource")}
					</Label>
					<Input
						id={`${id}-resource`}
						value={resource}
						onChange={(e) => setResource(e.target.value)}
						placeholder="project/<id>/task/<id>"
						autoComplete="off"
						spellCheck={false}
						className="font-mono text-xs"
					/>
				</div>
			</div>
			<div className="flex flex-col gap-1.5">
				<Label htmlFor={`${id}-attrs`} className="text-xs">
					{t("simulate.attributes")}
				</Label>
				<Textarea
					id={`${id}-attrs`}
					value={attrText}
					onChange={(e) => setAttrText(e.target.value)}
					placeholder={"task.sprint_id=<sprint id>"}
					spellCheck={false}
					className="min-h-14 font-mono text-xs"
				/>
				<p className="text-xs text-muted-foreground">
					{t("simulate.attributesHint")}
				</p>
				{attrError !== null ? (
					<p role="alert" className="text-xs text-destructive">
						{t("simulate.badAttribute", { line: attrError })}
					</p>
				) : null}
			</div>
			<div className="flex items-center gap-3">
				<Button
					type="button"
					size="sm"
					variant="secondary"
					onClick={run}
					disabled={!ready || mutation.isPending}
				>
					{mutation.isPending ? t("simulate.running") : t("simulate.run")}
				</Button>
				{!policy ? (
					<span className="text-xs text-muted-foreground">
						{t("simulate.needsValidJson")}
					</span>
				) : null}
			</div>

			{mutation.isError ? (
				<p role="alert" className="text-xs text-destructive">
					{t("simulate.failed")}
				</p>
			) : null}

			{result ? (
				<div
					role="status"
					className="flex flex-col gap-1.5 rounded-md border bg-background p-3 text-xs"
				>
					<div
						className={`flex items-center gap-1.5 text-sm font-semibold ${
							result.allowed
								? "text-emerald-700 dark:text-emerald-400"
								: "text-destructive"
						}`}
					>
						{result.allowed ? (
							<CircleCheck className="size-4" aria-hidden="true" />
						) : (
							<CircleX className="size-4" aria-hidden="true" />
						)}
						{result.allowed ? t("simulate.allowed") : t("simulate.denied")}
					</div>
					<p>
						{decider
							? t("simulate.decidedBy", {
									statement: decider.sid || `statements[${decider.index}]`,
									effect: decider.effect,
								})
							: t("simulate.noStatement")}
					</p>
					{result.matched.length > 1 ? (
						<ul className="text-muted-foreground">
							{result.matched.map((m) => (
								<li key={`${m.role_id}:${m.index}`}>
									<code className="font-mono">
										{m.sid || `statements[${m.index}]`}
									</code>{" "}
									({m.effect})
								</li>
							))}
						</ul>
					) : null}
				</div>
			) : null}
		</section>
	);
}
