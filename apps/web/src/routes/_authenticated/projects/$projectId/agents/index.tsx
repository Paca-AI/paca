import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Bot, Plus } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { AgentCard } from "@/components/projects/agents/agent-card";
import {
	AcpSetupDialog,
	CreateAgentDialog,
} from "@/components/projects/agents/create-agent-dialog";
import { NoPermissionState } from "@/components/shared/no-permission-state";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import {
	type AcpBridgeToken,
	type Agent,
	llmModelsQueryOptions,
	projectScopedAgentsQueryOptions,
} from "@/lib/agent-api";
import { isForbiddenError } from "@/lib/api-error";
import { projectQueryOptions } from "@/lib/project-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/agents/",
)({
	validateSearch: (search: Record<string, unknown>) => ({
		create: search.create === true || search.create === "true",
	}),
	// Neither projectRolesQueryOptions (see below) nor the agent list itself
	// is prefetched here — a role holding only agents.write (no agents.read)
	// or only tasks.write (no project.roles.read) is an unusual but valid
	// combination, and prefetching either in the loader would crash this
	// entire page over one missing permission instead of showing
	// NoPermissionState in place of just the agent grid below.
	loader: async ({ context: { queryClient } }) => {
		await queryClient.ensureQueryData(llmModelsQueryOptions);
	},
	component: AgentsPage,
});

// ── Page ──────────────────────────────────────────────────────────────────────

function AgentsPage() {
	const { t } = useTranslation("projects");
	const { projectId } = Route.useParams();
	const { create } = Route.useSearch();
	const navigate = Route.useNavigate();
	const { hasProjectPermission, isLoading: isPermissionsLoading } =
		useProjectPermissions(projectId);
	const canWrite = hasProjectPermission("agents.write");
	const canRead = hasProjectPermission("agents.read");
	// Creating an agent also grants it project membership at the chosen role,
	// so the API requires project.members.write in addition to agents.write
	// (GHSA-xxc8-ggm7-vmxp) — gate the create affordances on both so this
	// button doesn't invite a 403 for an agents.write-only Editor. Managing
	// an *existing* agent (AgentCard below, ACP bridge regen) stays on
	// canWrite alone — those routes weren't changed.
	const canCreate = canWrite && hasProjectPermission("project.members.write");

	const { data: project } = useQuery(projectQueryOptions(projectId));
	// projectScopedAgentsQueryOptions server-side-filters out global-scope
	// agents invited into this project as members (see agent-api.ts's
	// AgentScope doc comment) — this page manages project-owned agents only;
	// global agents are configured from /admin/agents.
	const {
		data: agents = [],
		isLoading: isDataLoading,
		isError,
		error,
	} = useQuery({
		...projectScopedAgentsQueryOptions(projectId),
		enabled: canRead,
	});
	// While permissions are still loading, canRead defaults to false same as
	// a confirmed denial — guard on isPermissionsLoading (and fold it into
	// isLoading) so the page shows the skeleton instead of flashing
	// NoPermissionState first.
	const isLoading = isPermissionsLoading || isDataLoading;
	const noPermission =
		!isPermissionsLoading && (!canRead || (isError && isForbiddenError(error)));
	const [createOpen, setCreateOpen] = useState(create);
	const [acpSetupAgent, setAcpSetupAgent] = useState<Agent | null>(null);
	const [acpSetupToken, setAcpSetupToken] = useState<AcpBridgeToken | null>(
		null,
	);
	const [acpSetupKey, setAcpSetupKey] = useState<string | null>(null);

	// `?create=true` (from the agent picker's "no agents yet" empty state)
	// only needs to open the dialog once — leaving it in the URL would
	// reopen the dialog on every refresh/back-navigation after the user
	// closes it, so strip it once the dialog's opened state is consumed.
	function handleCreateOpenChange(nextOpen: boolean) {
		setCreateOpen(nextOpen);
		if (!nextOpen && create) {
			navigate({
				search: (prev) => ({ ...prev, create: false }),
				replace: true,
			});
		}
	}

	return (
		<div className="flex flex-col">
			{/* Header */}
			<div className="relative overflow-hidden border-b border-border/50">
				<div
					className="pointer-events-none absolute inset-0 opacity-50"
					style={{
						backgroundImage:
							"radial-gradient(circle, color-mix(in oklch, var(--color-primary) 12%, transparent) 1px, transparent 1px)",
						backgroundSize: "20px 20px",
						maskImage:
							"radial-gradient(ellipse 70% 100% at 0% 0%, black 20%, transparent 70%)",
					}}
				/>
				<div className="relative flex items-end justify-between px-6 py-8">
					<div>
						<h1 className="font-[Syne] text-2xl font-bold tracking-tight">
							{t("agents.page.title")}
						</h1>
						<p className="mt-1 text-sm text-muted-foreground">
							{project?.name} · {t("agents.page.subtitle")}
						</p>
					</div>
					{canCreate ? (
						<Button
							size="sm"
							className="gap-1.5 shadow-sm shadow-primary/20"
							onClick={() => setCreateOpen(true)}
						>
							<Plus className="size-3.5" />
							{t("agents.page.newAgent")}
						</Button>
					) : null}
				</div>
			</div>

			{/* Content */}
			<div className="p-6">
				{noPermission ? (
					<NoPermissionState
						icon={Bot}
						title={t("agents.page.noPermission.title")}
						description={t("agents.page.noPermission.description")}
					/>
				) : isLoading ? (
					<div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
						{Array.from({ length: 3 }).map((_, i) => (
							// biome-ignore lint/suspicious/noArrayIndexKey: skeleton
							<Skeleton key={i} className="h-36 rounded-xl" />
						))}
					</div>
				) : agents.length === 0 ? (
					<div className="flex flex-col items-center justify-center gap-4 py-20 text-center">
						<div className="flex size-16 items-center justify-center rounded-2xl bg-muted/50">
							<Bot className="size-8 text-muted-foreground/50" />
						</div>
						<div>
							<p className="font-medium text-sm">
								{t("agents.page.empty.title")}
							</p>
							<p className="text-xs text-muted-foreground mt-1 max-w-xs">
								{t("agents.page.empty.description")}
							</p>
						</div>
						{canCreate && (
							<Button size="sm" onClick={() => setCreateOpen(true)}>
								<Plus className="size-4 mr-1.5" />
								{t("agents.page.empty.createFirstAgent")}
							</Button>
						)}
					</div>
				) : (
					<div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
						{agents.map((agent) => (
							<AgentCard
								key={agent.id}
								agent={agent}
								projectId={projectId}
								canWrite={canWrite}
							/>
						))}
					</div>
				)}
			</div>

			<CreateAgentDialog
				projectId={projectId}
				open={createOpen}
				onOpenChange={handleCreateOpenChange}
				onAcpAgentCreated={(agent, token, mcpKey) => {
					setAcpSetupAgent(agent);
					setAcpSetupToken(token);
					setAcpSetupKey(mcpKey);
				}}
			/>
			<AcpSetupDialog
				projectId={projectId}
				agent={acpSetupAgent}
				token={acpSetupToken}
				mcpKey={acpSetupKey}
				open={acpSetupAgent !== null}
				canWrite={canWrite}
				onOpenChange={(v) => {
					if (!v) {
						setAcpSetupAgent(null);
						setAcpSetupToken(null);
						setAcpSetupKey(null);
					}
				}}
				onTokenGenerated={() =>
					setAcpSetupAgent((a) =>
						a ? { ...a, has_acp_bridge_token: true } : a,
					)
				}
			/>
		</div>
	);
}
