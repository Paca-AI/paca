import {
	type QueryClient,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { Check, Headphones, Lightbulb, Plus, X } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import { currentUserQueryOptions } from "@/lib/auth-api";
import { chatErrorKey } from "@/lib/chat-errors";
import {
	type CallAssistant,
	callAssistantsQueryOptions,
	channelProposalsQueryOptions,
	claimProposal,
	inviteAssistant,
	type MeetingAgent,
	meetingAgentsQueryOptions,
	meetingConfigQueryOptions,
	meetingKeys,
	type Proposal,
	type ProposalChip,
	proposalQueryOptions,
	removeAssistant,
	resolveProposal,
} from "@/lib/meeting-api";
import { executeProposal, type ProposalPayload } from "@/lib/proposal-actions";
import { useCall } from "./call-provider";

function refresh(qc: QueryClient, projectId: string) {
	void qc.invalidateQueries({ queryKey: ["meeting", projectId] });
}

// ---------------------------------------------------------------- assistants

/**
 * The assistants in the call: who is listening and what they keep (the
 * consent indicator is mandatory), a picker to bring one in, and the button
 * that asks for suggestions.
 */
export function AssistantSection({
	projectId,
	channelId,
	canInvite,
	canModerate,
}: {
	projectId: string;
	channelId: string;
	/** The caller may invite assistants (calls:invite_agent on the project). */
	canInvite: boolean;
	canModerate: boolean;
}) {
	const { t } = useTranslation("chat");
	const qc = useQueryClient();
	const call = useCall();
	const enabled = useQuery(meetingConfigQueryOptions).data === true;
	const assistants = useQuery(
		callAssistantsQueryOptions(projectId, channelId, enabled),
	).data;
	const [error, setError] = useState<string | null>(null);
	const [note, setNote] = useState<string | null>(null);

	const suggest = useMutation({
		mutationFn: () => call.suggest(),
		onMutate: () => setNote(null),
		onSuccess: (n) => {
			setError(null);
			setNote(
				n > 0
					? t("assistant.suggested", { count: n })
					: t("assistant.nothingToSuggest"),
			);
		},
		onError: () => setError(t("assistant.suggestFailed")),
	});
	const remove = useMutation({
		mutationFn: (agentId: string) =>
			removeAssistant(projectId, channelId, agentId),
		onError: (e) => setError(t(`errors.${chatErrorKey(e)}`)),
		onSuccess: () => {
			setError(null);
			refresh(qc, projectId);
		},
	});

	if (!enabled) return null;
	const active = (assistants ?? []).filter(
		(a) => a.status === "dispatched" || a.status === "active",
	);
	if (active.length === 0 && !canInvite) return null;
	const canSuggest = active.some(
		(a) => a.status === "active" && a.mode !== "notes_only",
	);

	return (
		<section
			aria-label={t("assistant.sectionLabel")}
			className="mt-3 space-y-2 rounded-lg border bg-muted/30 p-3"
		>
			<div className="flex flex-wrap items-center gap-2">
				<Headphones className="size-4 text-muted-foreground" aria-hidden />
				<h2 className="text-sm font-medium">{t("assistant.title")}</h2>
				<div className="ml-auto flex items-center gap-2">
					{canSuggest ? (
						<Button
							size="sm"
							variant="secondary"
							onClick={() => suggest.mutate()}
							disabled={suggest.isPending}
						>
							{suggest.isPending ? <Spinner aria-hidden /> : <Lightbulb />}
							{t("assistant.suggest")}
						</Button>
					) : null}
					{canInvite ? (
						<InvitePicker
							projectId={projectId}
							channelId={channelId}
							already={active.map((a) => a.agent_id)}
							onError={(m) => setError(m)}
						/>
					) : null}
				</div>
			</div>
			{active.map((a) => (
				<AssistantRow
					key={a.agent_id}
					assistant={a}
					canRemove={canModerate}
					onRemove={() => remove.mutate(a.agent_id)}
				/>
			))}
			{active.length === 0 ? (
				<p className="text-xs text-muted-foreground">{t("assistant.none")}</p>
			) : null}
			{note ? (
				<p role="status" className="text-xs text-muted-foreground">
					{note}
				</p>
			) : null}
			{error ? (
				<p role="alert" className="text-xs text-destructive">
					{error}
				</p>
			) : null}
		</section>
	);
}

function AssistantRow({
	assistant: a,
	canRemove,
	onRemove,
}: {
	assistant: CallAssistant;
	canRemove: boolean;
	onRemove: () => void;
}) {
	const { t } = useTranslation("chat");
	const listening = a.status === "active";
	return (
		<div className="flex items-start gap-2 rounded-md bg-background p-2 text-xs">
			<span
				aria-hidden
				className={
					listening
						? "mt-1 size-2 shrink-0 rounded-full bg-emerald-500"
						: "mt-1 size-2 shrink-0 animate-pulse rounded-full bg-amber-500"
				}
			/>
			<div className="min-w-0 flex-1">
				<p className="font-medium">
					{listening
						? t(
								a.mode === "notes_only"
									? "assistant.listeningNotes"
									: "assistant.listening",
								{ name: a.name },
							)
						: t("assistant.joining", { name: a.name })}
				</p>
				<p className="text-muted-foreground">
					{a.store_transcript
						? t("assistant.keeps", { days: a.transcript_retention_days })
						: t("assistant.keepsNothing")}
				</p>
				{a.announcement ? (
					<p className="mt-1 text-muted-foreground">{a.announcement}</p>
				) : null}
			</div>
			{canRemove ? (
				<Button
					size="icon-sm"
					variant="ghost"
					aria-label={t("assistant.remove", { name: a.name })}
					onClick={onRemove}
				>
					<X />
				</Button>
			) : null}
		</div>
	);
}

function InvitePicker({
	projectId,
	channelId,
	already,
	onError,
}: {
	projectId: string;
	channelId: string;
	already: string[];
	onError: (message: string) => void;
}) {
	const { t } = useTranslation("chat");
	const qc = useQueryClient();
	const [open, setOpen] = useState(false);
	const agents = useQuery(meetingAgentsQueryOptions(projectId, open)).data;
	const invite = useMutation({
		mutationFn: (agentId: string) =>
			inviteAssistant(projectId, channelId, agentId),
		onError: (e) => onError(t(`errors.${chatErrorKey(e)}`)),
		onSuccess: () => {
			setOpen(false);
			refresh(qc, projectId);
		},
	});
	const choices = (agents ?? []).filter((a) => !already.includes(a.id));
	return (
		<Popover open={open} onOpenChange={setOpen}>
			<PopoverTrigger
				render={<Button size="sm" variant="outline" />}
				aria-label={t("assistant.invite")}
			>
				<Plus />
				{t("assistant.invite")}
			</PopoverTrigger>
			<PopoverContent align="end" className="w-80 space-y-2">
				<p className="text-xs text-muted-foreground">
					{t("assistant.pickerHint")}
				</p>
				{agents === undefined ? (
					<Spinner aria-hidden />
				) : choices.length === 0 ? (
					<p className="text-sm">{t("assistant.noneToInvite")}</p>
				) : (
					<ul className="space-y-1">
						{choices.map((a) => (
							<li key={a.id}>
								<button
									type="button"
									className="w-full rounded-md border p-2 text-left text-sm hover:bg-muted disabled:opacity-50"
									disabled={invite.isPending}
									onClick={() => invite.mutate(a.id)}
								>
									<span className="font-medium">{a.name}</span>
									<span className="block text-xs text-muted-foreground">
										{privacySummary(a, t)}
									</span>
								</button>
							</li>
						))}
					</ul>
				)}
			</PopoverContent>
		</Popover>
	);
}

function privacySummary(a: MeetingAgent, t: TFunction<"chat">): string {
	const kept = a.store_transcript
		? t("assistant.picker.keeps", { days: a.transcript_retention_days })
		: t("assistant.picker.keepsNothing");
	return `${t(`assistant.picker.mode.${a.mode}`)} · ${kept}`;
}

// ------------------------------------------------------------------ captions

/** The latest live captions, above the controls. */
export function CaptionsStrip({ names }: { names: Map<string, string> }) {
	const { t } = useTranslation("chat");
	const { captions } = useCall();
	if (captions.length === 0) return null;
	return (
		<div
			role="log"
			aria-label={t("assistant.captions")}
			className="mt-3 space-y-0.5 rounded-md bg-black/70 px-3 py-2 text-sm text-white"
		>
			{captions.map((c) => (
				<p key={c.id}>
					<span className="font-medium">
						{names.get(c.speakerId) ?? t("assistant.someone")}:
					</span>{" "}
					{c.text}
				</p>
			))}
		</div>
	);
}

// ----------------------------------------------------------------- proposals

/**
 * The Assistant panel: proposals waiting for this person as editable cards, and
 * a read-only line for the ones other people are reviewing.
 */
export function ProposalsSection({
	projectId,
	channelId,
	names,
}: {
	projectId: string;
	channelId: string;
	names: Map<string, string>;
}) {
	const { t } = useTranslation("chat");
	const enabled = useQuery(meetingConfigQueryOptions).data === true;
	const me = useQuery(currentUserQueryOptions).data;
	const chips = useQuery(
		channelProposalsQueryOptions(projectId, channelId, enabled),
	).data;
	if (!enabled || !chips || chips.length === 0) return null;
	const mine = chips.filter(
		(c) =>
			c.target_account_id === me?.id &&
			(c.status === "pending" || c.status === "claimed"),
	);
	const others = chips.filter(
		(c) => c.target_account_id !== me?.id && c.status === "pending",
	);
	if (mine.length === 0 && others.length === 0) return null;
	return (
		<section
			aria-label={t("proposals.sectionLabel")}
			className="mt-3 space-y-2"
		>
			{mine.map((c) => (
				<ProposalCard key={c.id} projectId={projectId} chip={c} />
			))}
			{others.map((c) => (
				<p key={c.id} className="text-xs text-muted-foreground">
					{t("proposals.othersReviewing", {
						agent: c.agent_name,
						kind: t(`proposals.kinds.${c.kind}`),
						name: names.get(c.target_account_id) ?? t("assistant.someone"),
					})}
				</p>
			))}
		</section>
	);
}

function ProposalCard({
	projectId,
	chip,
}: {
	projectId: string;
	chip: ProposalChip;
}) {
	const proposal = useQuery(proposalQueryOptions(projectId, chip.id)).data;
	if (!proposal) {
		return (
			<div className="rounded-lg border p-3">
				<Spinner aria-hidden />
			</div>
		);
	}
	// Remounts with the saved fields if the proposal is refetched with new state.
	return (
		<ProposalForm
			key={`${proposal.id}:${proposal.status}`}
			projectId={projectId}
			proposal={proposal}
		/>
	);
}

function ProposalForm({
	projectId,
	proposal,
}: {
	projectId: string;
	proposal: Proposal;
}) {
	const { t } = useTranslation("chat");
	const qc = useQueryClient();
	const { reportResolved } = useCall();
	const [draft, setDraft] = useState<ProposalPayload>(proposal.payload);
	const [error, setError] = useState<string | null>(null);
	const set = (k: string, v: string) => setDraft((d) => ({ ...d, [k]: v }));
	const text = (k: string) =>
		typeof draft[k] === "string" ? (draft[k] as string) : "";

	const create = useMutation({
		mutationFn: async () => {
			// Claiming first stops a second tab or click from writing twice.
			await claimProposal(projectId, proposal.id);
			try {
				const result = await executeProposal(projectId, proposal.kind, draft);
				await resolveProposal(projectId, proposal.id, "resolved", result);
				return result;
			} catch (e) {
				// The write failed (for example this person may not create tasks):
				// record it, so the assistant is told and nobody retries blindly.
				await resolveProposal(projectId, proposal.id, "failed", {
					error: "write failed",
				}).catch(() => undefined);
				throw e;
			}
		},
		onSuccess: () => {
			setError(null);
			reportResolved(proposal.id, "resolved");
			refresh(qc, projectId);
			void qc.invalidateQueries({ queryKey: ["projects", projectId] });
		},
		onError: (e) => {
			setError(t(`errors.${chatErrorKey(e)}`));
			reportResolved(proposal.id, "failed");
			refresh(qc, projectId);
		},
	});
	const dismiss = useMutation({
		mutationFn: () => resolveProposal(projectId, proposal.id, "rejected"),
		onSuccess: () => {
			reportResolved(proposal.id, "rejected");
			refresh(qc, projectId);
		},
		onError: (e) => setError(t(`errors.${chatErrorKey(e)}`)),
	});
	const busy = create.isPending || dismiss.isPending;
	const canCreate =
		proposal.kind === "task.create"
			? text("title").trim() !== ""
			: proposal.kind === "doc.create_from_notes"
				? text("title").trim() !== "" && text("markdown").trim() !== ""
				: proposal.kind === "task.comment"
					? text("body").trim() !== ""
					: true;

	return (
		<div className="space-y-2 rounded-lg border bg-background p-3">
			<p className="text-xs text-muted-foreground">
				{t("proposals.from", {
					agent: proposal.agent_name,
					kind: t(`proposals.kinds.${proposal.kind}`),
				})}
			</p>
			{proposal.kind === "task.create" ? (
				<>
					<Field
						id={`${proposal.id}-title`}
						label={t("proposals.fields.title")}
					>
						<Input
							id={`${proposal.id}-title`}
							value={text("title")}
							onChange={(e) => set("title", e.target.value)}
						/>
					</Field>
					<Field
						id={`${proposal.id}-desc`}
						label={t("proposals.fields.description")}
					>
						<Textarea
							id={`${proposal.id}-desc`}
							rows={3}
							value={text("description")}
							onChange={(e) => set("description", e.target.value)}
						/>
					</Field>
					<Field label={t("proposals.fields.priority")}>
						<Select
							value={text("priority") || "none"}
							onValueChange={(v) =>
								set("priority", v === "none" ? "" : (v ?? ""))
							}
						>
							<SelectTrigger>
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								{(["none", "low", "medium", "high", "critical"] as const).map(
									(p) => (
										<SelectItem key={p} value={p}>
											{t(`proposals.priorities.${p}`)}
										</SelectItem>
									),
								)}
							</SelectContent>
						</Select>
					</Field>
				</>
			) : null}
			{proposal.kind === "task.comment" ? (
				<Field id={`${proposal.id}-body`} label={t("proposals.fields.comment")}>
					<Textarea
						id={`${proposal.id}-body`}
						rows={3}
						value={text("body")}
						onChange={(e) => set("body", e.target.value)}
					/>
				</Field>
			) : null}
			{proposal.kind === "doc.create_from_notes" ? (
				<>
					<Field id={`${proposal.id}-dt`} label={t("proposals.fields.title")}>
						<Input
							id={`${proposal.id}-dt`}
							value={text("title")}
							onChange={(e) => set("title", e.target.value)}
						/>
					</Field>
					<Field id={`${proposal.id}-md`} label={t("proposals.fields.notes")}>
						<Textarea
							id={`${proposal.id}-md`}
							rows={6}
							value={text("markdown")}
							onChange={(e) => set("markdown", e.target.value)}
						/>
					</Field>
				</>
			) : null}
			{proposal.kind === "task.update" ? (
				<ul className="space-y-0.5 text-sm">
					{Object.entries(proposal.payload)
						.filter(([, v]) => typeof v === "string")
						.map(([k, v]) => (
							<li key={k}>
								<span className="text-muted-foreground">
									{t(`proposals.updateFields.${k}`, { defaultValue: k })}:
								</span>{" "}
								{String(v) || t("proposals.cleared")}
							</li>
						))}
				</ul>
			) : null}
			{error ? (
				<p role="alert" className="text-xs text-destructive">
					{error}
				</p>
			) : null}
			<div className="flex items-center gap-2">
				<Button
					size="sm"
					onClick={() => create.mutate()}
					disabled={busy || !canCreate}
				>
					{create.isPending ? <Spinner aria-hidden /> : <Check />}
					{t(`proposals.confirm.${proposal.kind}`)}
				</Button>
				<Button
					size="sm"
					variant="ghost"
					onClick={() => dismiss.mutate()}
					disabled={busy}
				>
					{t("proposals.dismiss")}
				</Button>
			</div>
		</div>
	);
}

function Field({
	id,
	label,
	children,
}: {
	id?: string;
	label: string;
	children: React.ReactNode;
}) {
	return (
		<div className="space-y-1">
			<Label htmlFor={id} className="text-xs">
				{label}
			</Label>
			{children}
		</div>
	);
}

export { meetingKeys };
