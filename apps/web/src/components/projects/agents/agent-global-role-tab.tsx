import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ShieldCheck, ShieldOff } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { PermissionSummary } from "@/components/admin/global-roles/permission-summary";
import {
	isFullAccessRole,
	RolePicker,
} from "@/components/admin/global-roles/role-picker";
import { InlineNotice } from "@/components/shared/inline-notice";
import { RoleBadgeList, toRoleBadgeData } from "@/components/shared/role-badge";
import { Button } from "@/components/ui/button";
import { useCanAssignGlobalRole } from "@/hooks/use-can-assign-global-role";
import { usePermissions } from "@/hooks/use-permissions";
import { type Agent, globalAgentQueryOptions } from "@/lib/agent-api";
import { platformRolesQueryOptions, type Role } from "@/lib/role-api";
import { useSetAgentGlobalRole } from "./use-set-agent-global-role";

/** How many of the role's permissions to list before folding the rest into "+N". */
const SUMMARY_LIMIT = 12;

type Mode = "view" | "choose" | "confirm-remove";

/**
 * A global agent's global role: what it may do outside any project (from the
 * home and admin chat). Assigning or removing it is its own privilege, so it
 * lives on a tab of its own rather than in the create or edit forms, and
 * needs `roles:assign` on top of `agents:write`.
 */
export function AgentGlobalRoleTab({
	agent,
	canWrite,
}: {
	agent: Agent;
	/** Whether the viewer may write this agent (`agents:write`). */
	canWrite: boolean;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const { hasPermission } = usePermissions();
	const canAssignRole = useCanAssignGlobalRole();
	const canChange = canWrite && canAssignRole;

	// The agent only carries its roles' ids and names; what they grant comes
	// from the roles list, which needs roles:read.
	const { data: allRoles } = useQuery({
		...platformRolesQueryOptions,
		enabled: hasPermission("roles:read"),
	});
	const heldIds = (agent.roles ?? []).map((r) => r.id);
	const heldRoles = (allRoles ?? []).filter((r) => heldIds.includes(r.id));

	const [mode, setMode] = useState<Mode>("view");
	const [selected, setSelected] = useState<{
		ids: string[];
		roles: Role[];
	} | null>(null);

	const { setRoles, isPending, error, clearError } = useSetAgentGlobalRole({
		onChanged: () => {
			void qc.invalidateQueries({
				queryKey: globalAgentQueryOptions(agent.id).queryKey,
			});
			void qc.invalidateQueries({ queryKey: ["global-agents"] });
			backToView();
		},
	});

	const backToView = () => {
		setMode("view");
		setSelected(null);
		clearError();
	};

	// Picking the roles it already has changes nothing.
	const change =
		selected &&
		!(
			selected.ids.length === heldIds.length &&
			selected.ids.every((id) => heldIds.includes(id))
		)
			? selected
			: null;
	const addsFullAccess = !!change?.roles.some(
		(r) => !heldIds.includes(r.id) && isFullAccessRole(r),
	);
	const hasRole = heldIds.length > 0;

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
							heldRoles.length > 0 ? (
								heldRoles.map((role) => (
									<div key={role.id} className="flex flex-col gap-1.5">
										<RoleBadgeList
											roles={[toRoleBadgeData(role)]}
											className="[&_[data-kind]]:text-sm"
										/>
										{role.description ? (
											<p className="text-xs text-muted-foreground">
												{role.description}
											</p>
										) : null}
										<PermissionSummary role={role} limit={SUMMARY_LIMIT} />
									</div>
								))
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
							values={selected?.ids ?? heldIds}
							onChange={(ids, roles) => {
								setSelected({ ids, roles });
								clearError();
							}}
							currentRoleIds={heldIds}
							disabled={isPending}
						/>
						{addsFullAccess ? (
							<InlineNotice tone="warning">
								{t("agents.detail.globalRole.fullAccessWarning")}
							</InlineNotice>
						) : null}
						{error ? <InlineNotice tone="error">{error}</InlineNotice> : null}
						<div className="flex justify-end gap-2">
							<Button
								variant="outline"
								onClick={backToView}
								disabled={isPending}
							>
								{t("agents.detail.globalRole.cancel")}
							</Button>
							<Button
								onClick={() => change && setRoles(agent.id, change.ids)}
								disabled={!change || isPending}
							>
								{isPending
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
								disabled={isPending}
							>
								{t("agents.detail.globalRole.cancel")}
							</Button>
							<Button
								variant="destructive"
								onClick={() => setRoles(agent.id, [])}
								disabled={isPending}
							>
								{isPending
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
