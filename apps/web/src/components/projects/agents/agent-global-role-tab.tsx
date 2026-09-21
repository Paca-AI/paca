import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ShieldCheck, ShieldOff } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { PermissionSummary } from "@/components/admin/global-roles/permission-summary";
import {
	isFullAccessRole,
	RolePicker,
} from "@/components/admin/global-roles/role-picker";
import { InlineNotice } from "@/components/shared/inline-notice";
import { Button } from "@/components/ui/button";
import { useCanAssignGlobalRole } from "@/hooks/use-can-assign-global-role";
import { usePermissions } from "@/hooks/use-permissions";
import { type GlobalRole, globalRolesQueryOptions } from "@/lib/admin-api";
import {
	type Agent,
	clearGlobalAgentRole,
	globalAgentQueryOptions,
	setGlobalAgentRole,
} from "@/lib/agent-api";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";

/** How many of the role's permissions to list before folding the rest into "+N". */
const SUMMARY_LIMIT = 12;

type Mode = "view" | "choose" | "confirm-remove";

/**
 * A global agent's global role: what it may do outside any project (from the
 * home and admin chat). Assigning or removing it is its own privilege, so it
 * lives on a tab of its own rather than in the create or edit forms, and
 * needs `global_roles.assign` on top of `agents.write`.
 */
export function AgentGlobalRoleTab({
	agent,
	canWrite,
}: {
	agent: Agent;
	/** Whether the viewer may write this agent (`agents.write`). */
	canWrite: boolean;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const { hasPermission } = usePermissions();
	const canAssignRole = useCanAssignGlobalRole();
	const canChange = canWrite && canAssignRole;

	// The agent only carries the role's id; its name and permissions come from
	// the roles list, which needs global_roles.read.
	const { data: roles } = useQuery({
		...globalRolesQueryOptions,
		enabled: hasPermission("global_roles.read"),
	});
	const role = roles?.find((r) => r.id === agent.global_role_id) ?? null;

	const [mode, setMode] = useState<Mode>("view");
	const [selected, setSelected] = useState<GlobalRole | null>(null);
	const [error, setError] = useState<string | null>(null);

	const backToView = () => {
		setMode("view");
		setSelected(null);
		setError(null);
	};

	const mutation = useMutation({
		mutationFn: (next: GlobalRole | null) =>
			next
				? setGlobalAgentRole(agent.id, next.id)
				: clearGlobalAgentRole(agent.id),
		onSuccess: (updated) => {
			qc.setQueryData(globalAgentQueryOptions(agent.id).queryKey, updated);
			void qc.invalidateQueries({ queryKey: ["global-agents"] });
			backToView();
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			const messages: Partial<Record<string, string>> = {
				[ApiErrorCode.GlobalRoleNotFound]: t(
					"agents.detail.globalRole.errors.roleNotFound",
				),
				[ApiErrorCode.Forbidden]: t(
					"agents.detail.globalRole.errors.forbidden",
				),
			};
			setError(
				(code && messages[code]) ??
					t("agents.detail.globalRole.errors.generic"),
			);
		},
	});

	// Picking the role it already has changes nothing.
	const change =
		selected && selected.id !== agent.global_role_id ? selected : null;
	const hasRole = !!agent.global_role_id;

	return (
		<div className="max-w-2xl space-y-4">
			<p className="text-sm text-muted-foreground">
				{t("agents.detail.globalRole.description")}
			</p>

			<div className="space-y-4 rounded-xl border border-border/60 bg-card p-5">
				<div className="flex items-start gap-3">
					<div
						className={
							hasRole
								? "flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary"
								: "flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground"
						}
					>
						{hasRole ? (
							<ShieldCheck className="size-4" aria-hidden="true" />
						) : (
							<ShieldOff className="size-4" aria-hidden="true" />
						)}
					</div>
					<div className="flex min-w-0 flex-1 flex-col gap-1.5">
						{hasRole ? (
							role ? (
								<>
									<span className="font-mono text-sm font-semibold">
										{role.name}
									</span>
									<PermissionSummary role={role} limit={SUMMARY_LIMIT} />
								</>
							) : (
								<p className="text-sm text-muted-foreground">
									{t("agents.detail.globalRole.unknownRole")}
								</p>
							)
						) : (
							<>
								<span className="text-sm font-semibold">
									{t("agents.detail.globalRole.none")}
								</span>
								<span className="text-sm text-muted-foreground">
									{t("agents.detail.globalRole.noneHint")}
								</span>
							</>
						)}
					</div>
				</div>

				{mode === "choose" ? (
					<div className="flex flex-col gap-3 border-t pt-4">
						<RolePicker
							label={t("agents.detail.globalRole.title")}
							value={selected?.id ?? null}
							onChange={(next) => {
								setSelected(next);
								setError(null);
							}}
							currentRoleId={agent.global_role_id}
							disabled={mutation.isPending}
						/>
						{change && isFullAccessRole(change) ? (
							<InlineNotice tone="warning">
								{t("agents.detail.globalRole.fullAccessWarning")}
							</InlineNotice>
						) : null}
						{error ? <InlineNotice tone="error">{error}</InlineNotice> : null}
						<div className="flex justify-end gap-2">
							<Button
								variant="outline"
								onClick={backToView}
								disabled={mutation.isPending}
							>
								{t("agents.detail.globalRole.cancel")}
							</Button>
							<Button
								onClick={() => change && mutation.mutate(change)}
								disabled={!change || mutation.isPending}
							>
								{mutation.isPending
									? t("agents.detail.globalRole.assigning")
									: t("agents.detail.globalRole.assign")}
							</Button>
						</div>
					</div>
				) : mode === "confirm-remove" ? (
					<div className="flex flex-col gap-3 border-t pt-4">
						<InlineNotice tone="warning">
							{t("agents.detail.globalRole.removeConfirm")}
						</InlineNotice>
						{error ? <InlineNotice tone="error">{error}</InlineNotice> : null}
						<div className="flex justify-end gap-2">
							<Button
								variant="outline"
								onClick={backToView}
								disabled={mutation.isPending}
							>
								{t("agents.detail.globalRole.cancel")}
							</Button>
							<Button
								variant="destructive"
								onClick={() => mutation.mutate(null)}
								disabled={mutation.isPending}
							>
								{mutation.isPending
									? t("agents.detail.globalRole.removing")
									: t("agents.detail.globalRole.remove")}
							</Button>
						</div>
					</div>
				) : canChange ? (
					<div className="flex justify-end gap-2 border-t pt-4">
						{hasRole ? (
							<Button
								variant="ghost"
								className="text-destructive hover:text-destructive"
								onClick={() => setMode("confirm-remove")}
							>
								{t("agents.detail.globalRole.remove")}
							</Button>
						) : null}
						<Button
							variant={hasRole ? "outline" : "default"}
							onClick={() => setMode("choose")}
						>
							{hasRole
								? t("agents.detail.globalRole.change")
								: t("agents.detail.globalRole.assign")}
						</Button>
					</div>
				) : null}
			</div>

			{canChange ? null : (
				<InlineNotice tone="info">
					{t("agents.detail.globalRole.readOnly")}
				</InlineNotice>
			)}
		</div>
	);
}
