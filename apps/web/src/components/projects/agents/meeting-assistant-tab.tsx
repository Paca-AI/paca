import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Save } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { chatErrorKey } from "@/lib/chat-errors";
import {
	type DefaultTarget,
	type MeetingMode,
	type MeetingSettings,
	meetingKeys,
	meetingSettingsQueryOptions,
	PROPOSAL_KINDS,
	type ProposalKind,
	type SummaryTarget,
	updateMeetingSettings,
} from "@/lib/meeting-api";
import { notifyError } from "@/lib/notify";

const MODES: MeetingMode[] = ["on_demand", "auto_suggest", "notes_only"];
const SUMMARY_TARGETS: SummaryTarget[] = ["channel_message", "none"];
const DEFAULT_TARGETS: DefaultTarget[] = ["requester", "host"];

/** The Assistant tab of a meeting agent: how it listens, what it may propose and
 * what it keeps. The reasoning model and the instructions are on Overview. */
export function MeetingAssistantTab({
	projectId,
	agentId,
	canWrite,
}: {
	projectId: string;
	agentId: string;
	canWrite: boolean;
}) {
	const { data } = useQuery(meetingSettingsQueryOptions(projectId, agentId));
	if (!data) return <Skeleton className="h-64 w-full rounded-xl" />;
	// Re-mounts the form with the saved values after each save.
	return (
		<SettingsForm
			key={data.updated_at}
			settings={data}
			projectId={projectId}
			agentId={agentId}
			canWrite={canWrite}
		/>
	);
}

function SettingsForm({
	settings,
	projectId,
	agentId,
	canWrite,
}: {
	settings: MeetingSettings;
	projectId: string;
	agentId: string;
	canWrite: boolean;
}) {
	const { t } = useTranslation("projects");
	const { t: tChat } = useTranslation("chat");
	const qc = useQueryClient();
	const [sttProvider, setSttProvider] = useState(settings.stt_provider);
	const [sttModel, setSttModel] = useState(settings.stt_model);
	const [sttKey, setSttKey] = useState("");
	const [clearKey, setClearKey] = useState(false);
	const [sttBaseUrl, setSttBaseUrl] = useState(settings.stt_base_url);
	const [languages, setLanguages] = useState(settings.stt_languages.join(", "));
	const [mode, setMode] = useState(settings.mode);
	const [interval, setIntervalSeconds] = useState(
		String(settings.suggest_interval_seconds),
	);
	const [kinds, setKinds] = useState<ProposalKind[]>(settings.proposal_kinds);
	const [maxProposals, setMaxProposals] = useState(
		String(settings.max_proposals_per_call),
	);
	const [store, setStore] = useState(settings.store_transcript);
	const [retention, setRetention] = useState(
		String(settings.transcript_retention_days),
	);
	const [announcement, setAnnouncement] = useState(settings.announcement);
	const [summary, setSummary] = useState(settings.summary_target);
	const [target, setTarget] = useState(settings.default_target);

	const parsedLanguages = languages
		.split(/[,\s]+/)
		.map((l) => l.trim())
		.filter(Boolean);
	const num = (v: string) => Number.parseInt(v, 10);
	const valid =
		num(interval) >= 30 &&
		num(interval) <= 3600 &&
		num(maxProposals) >= 0 &&
		num(maxProposals) <= 100 &&
		num(retention) >= 1 &&
		num(retention) <= 3650;

	const save = useMutation({
		meta: { success: "updated", subject: "agentSettings" },
		onError: (err) => notifyError(err, tChat(`errors.${chatErrorKey(err)}`)),
		mutationFn: () =>
			updateMeetingSettings(projectId, agentId, {
				stt_provider: sttProvider,
				stt_model: sttModel.trim(),
				...(sttKey
					? { stt_api_key: sttKey }
					: clearKey
						? { stt_api_key: "" }
						: {}),
				stt_base_url: sttBaseUrl.trim(),
				stt_languages: parsedLanguages,
				mode,
				suggest_interval_seconds: num(interval),
				proposal_kinds: kinds,
				max_proposals_per_call: num(maxProposals),
				store_transcript: store,
				transcript_retention_days: num(retention),
				announcement: announcement.trim(),
				summary_target: summary,
				default_target: target,
			}),
		onSuccess: (updated) => {
			qc.setQueryData(meetingKeys.settings(projectId, agentId), updated);
			void qc.invalidateQueries({ queryKey: meetingKeys.agents(projectId) });
		},
	});

	const toggleKind = (k: ProposalKind, on: boolean) =>
		setKinds((cur) =>
			on ? [...cur.filter((x) => x !== k), k] : cur.filter((x) => x !== k),
		);
	const field = "assistant.fields";
	const disabled = !canWrite;

	return (
		<div className="max-w-2xl space-y-6">
			<p className="text-sm text-muted-foreground">
				{t("agents.detail.assistant.intro")}
			</p>

			<section className="space-y-3">
				<h2 className="text-sm font-medium">
					{t("agents.detail.assistant.listening")}
				</h2>
				<div className="grid grid-cols-2 gap-3">
					<div className="space-y-1.5">
						<Label>{t(`agents.detail.${field}.sttProvider`)}</Label>
						<Select
							value={sttProvider}
							onValueChange={(v) => setSttProvider(v ?? "")}
							disabled={disabled}
						>
							<SelectTrigger>
								<SelectValue
									placeholder={t(`agents.detail.${field}.sttProviderNone`)}
								/>
							</SelectTrigger>
							<SelectContent>
								{settings.stt_providers.map((p) => (
									<SelectItem key={p} value={p}>
										{p}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					</div>
					<div className="space-y-1.5">
						<Label htmlFor="stt-model">
							{t(`agents.detail.${field}.sttModel`)}
						</Label>
						<Input
							id="stt-model"
							value={sttModel}
							onChange={(e) => setSttModel(e.target.value)}
							disabled={disabled}
							placeholder="whisper-1"
						/>
					</div>
					<div className="space-y-1.5">
						<Label htmlFor="stt-key">
							{t(`agents.detail.${field}.sttKey`)}
						</Label>
						<Input
							id="stt-key"
							type="password"
							autoComplete="off"
							value={sttKey}
							onChange={(e) => {
								setSttKey(e.target.value);
								setClearKey(false);
							}}
							disabled={disabled || clearKey}
							placeholder={
								settings.has_stt_key
									? t(`agents.detail.${field}.sttKeyStored`)
									: ""
							}
						/>
						{settings.has_stt_key && canWrite ? (
							<label className="flex items-center gap-2 text-xs text-muted-foreground">
								<Switch
									checked={clearKey}
									onCheckedChange={(c) => {
										setClearKey(c);
										if (c) setSttKey("");
									}}
								/>
								{t(`agents.detail.${field}.sttKeyClear`)}
							</label>
						) : null}
					</div>
					<div className="space-y-1.5">
						<Label htmlFor="stt-base">
							{t(`agents.detail.${field}.sttBaseUrl`)}
						</Label>
						<Input
							id="stt-base"
							value={sttBaseUrl}
							onChange={(e) => setSttBaseUrl(e.target.value)}
							disabled={disabled}
							placeholder="http://whisper:8000/v1"
						/>
						<p className="text-xs text-muted-foreground">
							{t(`agents.detail.${field}.sttBaseUrlHint`)}
						</p>
					</div>
					<div className="col-span-2 space-y-1.5">
						<Label htmlFor="stt-langs">
							{t(`agents.detail.${field}.languages`)}
						</Label>
						<Input
							id="stt-langs"
							value={languages}
							onChange={(e) => setLanguages(e.target.value)}
							disabled={disabled}
							placeholder="en, vi"
						/>
						<p className="text-xs text-muted-foreground">
							{t(`agents.detail.${field}.languagesHint`)}
						</p>
					</div>
				</div>
			</section>

			<Separator />

			<section className="space-y-3">
				<h2 className="text-sm font-medium">
					{t("agents.detail.assistant.behavior")}
				</h2>
				<div className="space-y-1.5">
					<Label>{t(`agents.detail.${field}.mode`)}</Label>
					<Select
						value={mode}
						onValueChange={(v) => v && setMode(v as MeetingMode)}
						disabled={disabled}
					>
						<SelectTrigger>
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							{MODES.map((m) => (
								<SelectItem key={m} value={m}>
									{t(`agents.detail.assistant.modes.${m}`)}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
					<p className="text-xs text-muted-foreground">
						{t(`agents.detail.assistant.modeHints.${mode}`)}
					</p>
				</div>
				{mode === "auto_suggest" ? (
					<div className="space-y-1.5">
						<Label htmlFor="interval">
							{t(`agents.detail.${field}.interval`)}
						</Label>
						<Input
							id="interval"
							type="number"
							min={30}
							max={3600}
							value={interval}
							onChange={(e) => setIntervalSeconds(e.target.value)}
							disabled={disabled}
						/>
					</div>
				) : null}
				{mode !== "notes_only" ? (
					<>
						<fieldset className="space-y-2">
							<legend className="text-sm font-medium">
								{t(`agents.detail.${field}.kinds`)}
							</legend>
							{PROPOSAL_KINDS.map((k) => (
								<label key={k} className="flex items-center gap-2 text-sm">
									<Switch
										checked={kinds.includes(k)}
										onCheckedChange={(c) => toggleKind(k, c)}
										disabled={disabled}
									/>
									{t(`agents.detail.assistant.kindNames.${k}`)}
								</label>
							))}
							<p className="text-xs text-muted-foreground">
								{t(`agents.detail.${field}.kindsHint`)}
							</p>
						</fieldset>
						<div className="space-y-1.5">
							<Label htmlFor="max-proposals">
								{t(`agents.detail.${field}.maxProposals`)}
							</Label>
							<Input
								id="max-proposals"
								type="number"
								min={0}
								max={100}
								value={maxProposals}
								onChange={(e) => setMaxProposals(e.target.value)}
								disabled={disabled}
							/>
						</div>
						<div className="space-y-1.5">
							<Label>{t(`agents.detail.${field}.defaultTarget`)}</Label>
							<Select
								value={target}
								onValueChange={(v) => v && setTarget(v as DefaultTarget)}
								disabled={disabled}
							>
								<SelectTrigger>
									<SelectValue />
								</SelectTrigger>
								<SelectContent>
									{DEFAULT_TARGETS.map((x) => (
										<SelectItem key={x} value={x}>
											{t(`agents.detail.assistant.targets.${x}`)}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
						</div>
					</>
				) : null}
				<div className="space-y-1.5">
					<Label>{t(`agents.detail.${field}.summary`)}</Label>
					<Select
						value={summary}
						onValueChange={(v) => v && setSummary(v as SummaryTarget)}
						disabled={disabled}
					>
						<SelectTrigger>
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							{SUMMARY_TARGETS.map((x) => (
								<SelectItem key={x} value={x}>
									{t(`agents.detail.assistant.summaries.${x}`)}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</div>
			</section>

			<Separator />

			<section className="space-y-3">
				<h2 className="text-sm font-medium">
					{t("agents.detail.assistant.privacy")}
				</h2>
				<label className="flex items-center justify-between gap-3 text-sm">
					<span>
						{t(`agents.detail.${field}.storeTranscript`)}
						<span className="block text-xs text-muted-foreground">
							{t(`agents.detail.${field}.storeTranscriptHint`)}
						</span>
					</span>
					<Switch
						checked={store}
						onCheckedChange={setStore}
						disabled={disabled}
					/>
				</label>
				{store ? (
					<div className="space-y-1.5">
						<Label htmlFor="retention">
							{t(`agents.detail.${field}.retention`)}
						</Label>
						<Input
							id="retention"
							type="number"
							min={1}
							max={3650}
							value={retention}
							onChange={(e) => setRetention(e.target.value)}
							disabled={disabled}
						/>
					</div>
				) : null}
				<div className="space-y-1.5">
					<Label htmlFor="announcement">
						{t(`agents.detail.${field}.announcement`)}
					</Label>
					<Textarea
						id="announcement"
						value={announcement}
						onChange={(e) => setAnnouncement(e.target.value)}
						rows={3}
						maxLength={500}
						disabled={disabled}
						placeholder={t(`agents.detail.${field}.announcementPlaceholder`)}
					/>
				</div>
			</section>

			{canWrite ? (
				<div className="flex items-center gap-3 pt-2">
					<Button
						onClick={() => save.mutate()}
						disabled={!valid || save.isPending}
					>
						{save.isPending ? (
							<Spinner aria-hidden="true" className="mr-2" />
						) : (
							<Save className="mr-2 size-4" />
						)}
						{t("agents.detail.overview.saveChanges")}
					</Button>
				</div>
			) : null}
		</div>
	);
}
