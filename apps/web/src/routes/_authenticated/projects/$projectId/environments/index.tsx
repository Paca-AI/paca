import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { Plus, Server } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { EnvironmentCreateDialog } from "@/components/projects/environments/environment-create-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import {
	ENVIRONMENT_STATUS_COLORS,
	environmentsQueryOptions,
} from "@/lib/environment-api";
import { projectQueryOptions } from "@/lib/project-api";
import { cn } from "@/lib/utils";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/environments/",
)({
	validateSearch: (search: Record<string, unknown>) => ({
		create: search.create === true || search.create === "true",
	}),
	loader: async ({ context: { queryClient }, params: { projectId } }) => {
		await queryClient.ensureQueryData(environmentsQueryOptions(projectId));
	},
	component: EnvironmentsPage,
});

function EnvironmentsPage() {
	const { t } = useTranslation("projects");
	const { projectId } = Route.useParams();
	const { create } = Route.useSearch();
	const navigate = Route.useNavigate();
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const canWrite = hasProjectPermission("environments.write");

	const { data: project } = useQuery(projectQueryOptions(projectId));
	const { data: environments = [], isLoading } = useQuery(
		environmentsQueryOptions(projectId),
	);
	const [createOpen, setCreateOpen] = useState(create);

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
							{t("environments.page.title")}
						</h1>
						<p className="mt-1 text-sm text-muted-foreground">
							{project?.name} · {t("environments.page.subtitle")}
						</p>
					</div>
					{canWrite ? (
						<Button
							size="sm"
							className="gap-1.5 shadow-sm shadow-primary/20"
							onClick={() => setCreateOpen(true)}
						>
							<Plus className="size-3.5" />
							{t("environments.page.newEnvironment")}
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
							<Skeleton key={i} className="h-32 rounded-xl" />
						))}
					</div>
				) : environments.length === 0 ? (
					<div className="flex flex-col items-center justify-center gap-4 py-20 text-center">
						<div className="flex size-16 items-center justify-center rounded-2xl bg-muted/50">
							<Server className="size-8 text-muted-foreground/50" />
						</div>
						<div>
							<p className="font-medium text-sm">
								{t("environments.page.empty.title")}
							</p>
							<p className="text-xs text-muted-foreground mt-1 max-w-xs">
								{t("environments.page.empty.description")}
							</p>
						</div>
						{canWrite && (
							<Button size="sm" onClick={() => setCreateOpen(true)}>
								<Plus className="size-4 mr-1.5" />
								{t("environments.page.empty.createFirstEnvironment")}
							</Button>
						)}
					</div>
				) : (
					<div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
						{environments.map((env) => (
							<Link
								key={env.id}
								to="/projects/$projectId/environments/$environmentId"
								params={{ projectId, environmentId: env.id }}
								className="group flex flex-col gap-3 rounded-xl border border-border/60 bg-card p-5 transition-all hover:border-border hover:shadow-sm"
							>
								<div className="flex items-start justify-between gap-3">
									<div className="flex items-center gap-3 min-w-0">
										<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10">
											<Server className="size-4.5 text-primary" />
										</div>
										<div className="min-w-0">
											<p className="font-semibold text-sm leading-tight truncate">
												{env.name}
											</p>
											<p className="text-xs text-muted-foreground mt-0.5 font-mono truncate">
												{env.slug}
											</p>
										</div>
									</div>
									<Badge
										variant="secondary"
										className="text-xs font-medium shrink-0"
									>
										{env.backend}
									</Badge>
								</div>
								<div className="flex items-center gap-1.5 text-xs text-muted-foreground">
									{env.status === "running" && (
										<span
											className={cn(
												"size-1.5 rounded-full",
												ENVIRONMENT_STATUS_COLORS[env.status].replace(
													"text-",
													"bg-",
												),
											)}
										/>
									)}
									<span className={ENVIRONMENT_STATUS_COLORS[env.status]}>
										{t(`environments.status.${env.status}`)}
									</span>
								</div>
							</Link>
						))}
					</div>
				)}
			</div>

			<EnvironmentCreateDialog
				projectId={projectId}
				open={createOpen}
				onOpenChange={handleCreateOpenChange}
			/>
		</div>
	);
}
