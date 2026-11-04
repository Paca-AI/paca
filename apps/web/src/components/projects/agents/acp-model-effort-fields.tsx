import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";

// Reasoning-effort values the ACP adapters commonly offer. Effort values are
// agent-defined, so this is not exhaustive: a stored value outside it (e.g.
// set via the API) is appended to the list so it still displays, and a value
// the running session doesn't support is skipped by the bridge (provider
// default is used).
const EFFORT_LEVELS = ["low", "medium", "high", "xhigh", "max"] as const;

// Select can't hold "" as an item value, so the "provider default" choice
// travels through the UI under this sentinel and maps back to "".
const DEFAULT_EFFORT = "default";

interface AcpModelEffortFieldsProps {
	idPrefix: string;
	model: string;
	effort: string;
	onModelChange: (model: string) => void;
	onEffortChange: (effort: string) => void;
	disabled?: boolean;
}

/** Optional model + reasoning-effort overrides for an ACP agent. */
export function AcpModelEffortFields({
	idPrefix,
	model,
	effort,
	onModelChange,
	onEffortChange,
	disabled,
}: AcpModelEffortFieldsProps) {
	const { t } = useTranslation("projects");
	return (
		<>
			<div className="space-y-1.5">
				<Label htmlFor={`${idPrefix}-acp-model`}>
					{t("agents.acpModelEffort.modelLabel")}
				</Label>
				<Input
					id={`${idPrefix}-acp-model`}
					placeholder={t("agents.acpModelEffort.modelPlaceholder")}
					value={model}
					onChange={(e) => onModelChange(e.target.value)}
					disabled={disabled}
				/>
				<p className="text-xs text-muted-foreground">
					{t("agents.acpModelEffort.modelHint")}
				</p>
			</div>
			<div className="space-y-1.5">
				<Label htmlFor={`${idPrefix}-acp-effort`}>
					{t("agents.acpModelEffort.effortLabel")}
				</Label>
				<Select
					value={effort || DEFAULT_EFFORT}
					onValueChange={(v) =>
						onEffortChange(v === DEFAULT_EFFORT ? "" : (v ?? ""))
					}
					disabled={disabled}
				>
					<SelectTrigger id={`${idPrefix}-acp-effort`}>
						<SelectValue />
					</SelectTrigger>
					<SelectContent>
						<SelectItem value={DEFAULT_EFFORT}>
							{t("agents.acpModelEffort.effortDefault")}
						</SelectItem>
						{EFFORT_LEVELS.map((level) => (
							<SelectItem key={level} value={level}>
								{t(`agents.acpModelEffort.effort_${level}`)}
							</SelectItem>
						))}
						{effort &&
							!(EFFORT_LEVELS as readonly string[]).includes(effort) && (
								<SelectItem value={effort}>{effort}</SelectItem>
							)}
					</SelectContent>
				</Select>
				<p className="text-xs text-muted-foreground">
					{t("agents.acpModelEffort.effortHint")}
				</p>
			</div>
		</>
	);
}
