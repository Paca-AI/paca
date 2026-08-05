import { useQuery } from "@tanstack/react-query";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { Bot, Plus } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { AgentCard } from "@/components/projects/agents/agent-card";
import {
	AcpSetupDialog,
	CreateAgentDialog,
} from "@/components/projects/agents/create-agent-dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { usePermissions } from "@/hooks/use-permissions";
import { myPermissionsQueryOptions } from "@/lib/admin-api";
import {
	type AcpBridgeToken,
	type Agent,
	globalAgentsQueryOptions,
	llmModelsQueryOptions,
} from "@/lib/agent-api";
import { hasPermission } from "@/lib/permissions";

export const Route = createFileRoute("/_authenticated/admin/agents/")({
	validateSearch: (search: Record<string, unknown>) => ({
		create: search.create === true || search.create === "true",
	}),
	beforeLoad: async ({ context: { queryClient } }) => {
		const permissions = await queryClient
			.fetchQuery(myPermissionsQueryOptions)
			.catch(() => [] as string[]);

		const canAccess =
			hasPermission(permissions, "agents.read") ||
			hasPermission(permissions, "agents.write");

		if (!canAccess) {
			throw redirect({ to: "/home" });
		}
	},
	loader: async ({ context: { queryClient } }) => {
		await Promise.all([
			queryClient.ensureQueryData(globalAgentsQueryOptions),
			queryClient.ensureQueryData(llmModelsQueryOptions),
		]);
	},
	component: GlobalAgentsPage,
});

// ── Page ───────────────────────────────────────────────────────────────────────
//
// Same layout as the project Agents page (routes/.../agents/index.tsx) —
// gradient hero header, card grid, loading/empty states — via the shared
// AgentCard component. See create-agent-dialog.tsx and agent-detail.tsx for
// the same reuse applied to the create flow and the detail page.

function GlobalAgentsPage() {
	const { t } = useTranslation("admin");
	const search = Route.useSearch();
	const navigate = Route.useNavigate();
	const { hasPermission } = usePermissions();
	const canWrite = hasPermission("agents.write");

	const { data: agents = [], isLoading } = useQuery(globalAgentsQueryOptions);

	const [createOpen, setCreateOpen] = useState(search.create);
	const [acpSetupAgent, setAcpSetupAgent] = useState<Agent | null>(null);
	const [acpSetupToken, setAcpSetupToken] = useState<AcpBridgeToken | null>(
		null,
	);

	// Mirrors the project Agents page's handling of ?create=true — only needs
	// to open the dialog once, so strip it from the URL once consumed.
	function handleCreateOpenChange(nextOpen: boolean) {
		setCreateOpen(nextOpen);
		if (!nextOpen && search.create) {
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
							{t("agents.header.title")}
						</h1>
						<p className="mt-1 text-sm text-muted-foreground">
							{t("agents.header.description")}
						</p>
					</div>
					{canWrite ? (
						<Button
							size="sm"
							className="gap-1.5 shadow-sm shadow-primary/20"
							onClick={() => setCreateOpen(true)}
						>
							<Plus className="size-3.5" />
							{t("agents.header.newAgent")}
						</Button>
					) : null}
				</div>
			</div>

			{/* Content */}
			<div className="p-6">
				{isLoading ? (
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
							<p className="font-medium text-sm">{t("agents.empty.title")}</p>
							<p className="text-xs text-muted-foreground mt-1 max-w-xs">
								{t("agents.empty.description")}
							</p>
						</div>
						{canWrite && (
							<Button size="sm" onClick={() => setCreateOpen(true)}>
								<Plus className="size-4 mr-1.5" />
								{t("agents.empty.createAgent")}
							</Button>
						)}
					</div>
				) : (
					<div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
						{agents.map((agent) => (
							<AgentCard key={agent.id} agent={agent} canWrite={canWrite} />
						))}
					</div>
				)}
			</div>

			{canWrite && (
				<CreateAgentDialog
					open={createOpen}
					onOpenChange={handleCreateOpenChange}
					onAcpAgentCreated={(agent, token) => {
						setAcpSetupAgent(agent);
						setAcpSetupToken(token);
					}}
				/>
			)}
			<AcpSetupDialog
				agent={acpSetupAgent}
				token={acpSetupToken}
				open={acpSetupAgent !== null}
				canWrite={canWrite}
				onOpenChange={(v) => {
					if (!v) {
						setAcpSetupAgent(null);
						setAcpSetupToken(null);
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
