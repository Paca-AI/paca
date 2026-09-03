import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import {
	AlertCircle,
	ArrowLeft,
	ExternalLink,
	Loader2,
	MessageSquare,
	Server,
	Trash2,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { PortForwardCommentsTab } from "@/components/projects/environments/port-forward-comments-tab";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { portForwardAnnotationsQueryOptions } from "@/lib/annotation-api";
import {
	deletePortForward,
	environmentConfigQueryOptions,
	environmentPortForwardsQueryOptions,
	environmentQueryOptions,
	portForwardQueryOptions,
	portForwardUrl,
} from "@/lib/environment-api";

// Reached from environment-detail.tsx's PortForwardsTab (each row links
// here) and from the extension's own "Open in Paca" link. Two tabs —
// Overview and Comments — hash-selected the same way EnvironmentDetailView
// does its own tab strip, rather than nested routes: neither tab needs its
// own deep link, only the port forward and an individual comment do (see
// routes/.../port-forwards/$portForwardId/{index,comments/$annotationId}.tsx).
type Tab = "overview" | "comments";

const TABS = [
	{
		id: "overview",
		labelKey: "portForwardDetail.tabs.overview",
		icon: Server,
	},
	{
		id: "comments",
		labelKey: "portForwardDetail.tabs.comments",
		icon: MessageSquare,
	},
] as const satisfies {
	id: Tab;
	labelKey: string;
	icon: React.ComponentType<{ className?: string }>;
}[];

export function PortForwardDetailView({
	projectId,
	environmentId,
	portForwardId,
}: {
	projectId: string;
	environmentId: string;
	portForwardId: string;
}) {
	const { t } = useTranslation("projects");
	const navigate = useNavigate();
	const qc = useQueryClient();
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const canWrite = hasProjectPermission("environments.write");

	const { data: environment } = useQuery(
		environmentQueryOptions(projectId, environmentId),
	);
	const { data: config } = useQuery(environmentConfigQueryOptions());
	const { data: portForward, isLoading } = useQuery(
		portForwardQueryOptions(projectId, environmentId, portForwardId),
	);
	// Only needed to show "this also deletes N comments" in the delete
	// dialog below — not rendered directly here (PortForwardCommentsTab
	// fetches its own copy via the same query key once the Comments tab is
	// actually shown).
	const { data: annotations = [] } = useQuery(
		portForwardAnnotationsQueryOptions(projectId, environmentId, portForwardId),
	);

	const [activeTab, setActiveTab] = useState<Tab>(() => {
		const hash = window.location.hash.slice(1);
		if (hash && TABS.map((tab) => tab.id).includes(hash as Tab)) {
			return hash as Tab;
		}
		return "overview";
	});
	const [confirmDelete, setConfirmDelete] = useState(false);

	useEffect(() => {
		const handleHashChange = () => {
			const hash = window.location.hash.slice(1);
			if (hash && TABS.map((tab) => tab.id).includes(hash as Tab)) {
				setActiveTab(hash as Tab);
			}
		};
		window.addEventListener("hashchange", handleHashChange);
		return () => window.removeEventListener("hashchange", handleHashChange);
	}, []);

	const handleTabChange = (tab: Tab) => {
		setActiveTab(tab);
		const url = new URL(window.location.href);
		url.hash = tab;
		window.history.pushState(null, "", url);
	};

	const deleteMutation = useMutation({
		mutationFn: () =>
			deletePortForward(projectId, environmentId, portForwardId),
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: environmentPortForwardsQueryOptions(projectId, environmentId)
					.queryKey,
			});
			navigate({
				to: "/projects/$projectId/environments/$environmentId",
				params: { projectId, environmentId },
				hash: "portForwards",
			});
		},
	});

	if (isLoading) {
		return (
			<div className="flex flex-col gap-4 p-6">
				<Skeleton className="h-16 w-full rounded-xl" />
				<Skeleton className="h-64 w-full rounded-xl" />
			</div>
		);
	}

	if (!portForward) {
		return (
			<div className="flex h-full flex-col items-center justify-center gap-4 text-muted-foreground/60">
				<AlertCircle className="size-10" />
				<div className="text-center">
					<p className="text-base font-medium text-foreground/70">
						{t("portForwardDetail.notFound.title")}
					</p>
					<p className="text-sm mt-1">
						{t("portForwardDetail.notFound.description")}
					</p>
				</div>
				<Link
					to="/projects/$projectId/environments/$environmentId"
					params={{ projectId, environmentId }}
					className="flex items-center gap-1.5 rounded-lg border border-border/60 px-4 py-2 text-sm font-medium text-foreground/70 hover:bg-muted/50 transition-colors mt-2"
				>
					<ArrowLeft className="size-4" />
					{t("portForwardDetail.notFound.backToEnvironment")}
				</Link>
			</div>
		);
	}

	const host = config?.port_forward_host || null;
	const hostPort = portForward.host_port;

	return (
		<div className="flex flex-col flex-1 min-h-0">
			{/* Header */}
			<div className="border-b border-border/50 px-6 py-5 shrink-0">
				<div className="flex items-center justify-between gap-4">
					<div className="flex items-center gap-2 min-w-0">
						<Link
							to="/projects/$projectId/environments/$environmentId"
							params={{ projectId, environmentId }}
							className="flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors shrink-0"
						>
							<ArrowLeft className="size-3.5" />
							{environment?.name ??
								t("portForwardDetail.notFound.backToEnvironment")}
						</Link>
						<span className="text-muted-foreground/40 shrink-0">/</span>
						<h1 className="text-lg font-semibold truncate">
							{portForward.label}
						</h1>
					</div>
					<div className="flex items-center gap-2 shrink-0">
						{hostPort !== null && host && (
							<Button
								variant="outline"
								onClick={() =>
									window.open(
										portForwardUrl(host, hostPort),
										"_blank",
										"noopener,noreferrer",
									)
								}
							>
								<ExternalLink className="size-3.5 mr-1.5" />
								{t("portForwardDetail.overview.open")}
							</Button>
						)}
						{canWrite && (
							<Button
								variant="outline"
								className="text-destructive hover:text-destructive"
								onClick={() => setConfirmDelete(true)}
							>
								<Trash2 className="size-3.5 mr-1.5" />
								{t("portForwardDetail.overview.delete")}
							</Button>
						)}
					</div>
				</div>
			</div>

			{/* Tabs */}
			<div className="border-b border-border/50 px-6 shrink-0">
				<div className="flex items-center gap-1 -mb-px">
					{TABS.map((tab) => {
						const Icon = tab.icon;
						const isActive = activeTab === tab.id;
						return (
							<button
								key={tab.id}
								type="button"
								onClick={() => handleTabChange(tab.id)}
								className={`flex items-center gap-1.5 px-3 py-2.5 text-sm font-medium border-b-2 transition-colors ${
									isActive
										? "border-primary text-primary"
										: "border-transparent text-muted-foreground hover:text-foreground"
								}`}
							>
								<Icon className="size-3.5" />
								{t(tab.labelKey)}
							</button>
						);
					})}
				</div>
			</div>

			{/* Tab content */}
			<div className="flex-1 overflow-auto p-6">
				{activeTab === "overview" && (
					<div className="max-w-xl rounded-lg border border-border/60 bg-card divide-y divide-border/60">
						<div className="flex items-center justify-between px-4 py-3">
							<span className="text-sm text-muted-foreground">
								{t("portForwardDetail.overview.containerPort", {
									port: portForward.container_port,
								})}
							</span>
						</div>
						<div className="flex items-center justify-between px-4 py-3">
							<span className="text-sm text-muted-foreground">
								{t("portForwardDetail.overview.hostPort")}
							</span>
							<span className="text-sm font-mono">
								{portForward.host_port !== null
									? portForward.host_port
									: t("portForwardDetail.overview.unassigned")}
							</span>
						</div>
						<div className="px-4 py-3 text-sm text-muted-foreground">
							{t("portForwardDetail.overview.createdAt", {
								date: new Date(portForward.created_at).toLocaleString(),
							})}
						</div>
					</div>
				)}
				{activeTab === "comments" && (
					<PortForwardCommentsTab
						projectId={projectId}
						environmentId={environmentId}
						portForwardId={portForwardId}
					/>
				)}
			</div>

			<Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
				<DialogContent className="max-w-sm">
					<DialogHeader>
						<DialogTitle>
							{t("portForwardDetail.overview.deleteDialog.title")}
						</DialogTitle>
						<DialogDescription>
							{t("portForwardDetail.overview.deleteDialog.description", {
								count: annotations.length,
							})}
						</DialogDescription>
					</DialogHeader>
					<DialogFooter>
						<Button
							variant="outline"
							onClick={() => setConfirmDelete(false)}
							disabled={deleteMutation.isPending}
						>
							{t("portForwardDetail.overview.deleteDialog.cancel")}
						</Button>
						<Button
							variant="destructive"
							onClick={() => deleteMutation.mutate()}
							disabled={deleteMutation.isPending}
						>
							{deleteMutation.isPending ? (
								<Loader2 className="size-4 animate-spin" />
							) : (
								t("portForwardDetail.overview.deleteDialog.confirm")
							)}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}
