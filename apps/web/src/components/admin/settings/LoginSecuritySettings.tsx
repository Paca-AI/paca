import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ReportIssueButton } from "@/components/report-issue-button";
import { InlineNotice } from "@/components/shared/inline-notice";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import {
	authSecurityPolicyQueryOptions,
	saveAuthSecurityPolicy,
} from "@/lib/mfa-api";
import { listRoles } from "@/lib/role-api";
import { publicSsoProvidersQueryOptions } from "@/lib/sso-api";

const ROLE_LIST_LIMIT = 100;
/** Server-side marker for "every role" in the protected-roles list. */
const ALL_ROLES = "*";

function parseList(text: string): string[] {
	return text
		.split(/[\n,]/)
		.map((s) => s.trim())
		.filter(Boolean);
}

/** Admin controls for who must use two-step verification and from where
 * password sign-in is allowed (both apply to the chosen "protected roles"),
 * and for SSO-only mode, which turns password sign-in off for everyone but the
 * default super admin. */
export function LoginSecuritySettings() {
	const { t } = useTranslation("admin");
	const queryClient = useQueryClient();
	const policyQuery = useQuery(authSecurityPolicyQueryOptions);
	const rolesQuery = useQuery({
		queryKey: ["admin", "roles", "names"],
		queryFn: () => listRoles(undefined, { pageSize: ROLE_LIST_LIMIT }),
	});
	// The enabled providers, as the login page lists them: SSO-only mode needs
	// at least one. Public, so it doesn't need the SSO settings permission.
	const ssoProvidersQuery = useQuery(publicSsoProvidersQueryOptions);

	const [requireMfa, setRequireMfa] = useState(false);
	const [allRoles, setAllRoles] = useState(true);
	const [roles, setRoles] = useState<string[]>([]);
	const [ips, setIps] = useState("");
	const [ssoOnly, setSsoOnly] = useState(false);
	const [error, setError] = useState<string | null>(null);

	useEffect(() => {
		const p = policyQuery.data;
		if (!p) return;
		setRequireMfa(p.require_mfa);
		const all = p.privileged_roles.includes(ALL_ROLES);
		setAllRoles(all);
		setRoles(all ? [] : p.privileged_roles);
		setIps(p.ip_allow_list.join("\n"));
		setSsoOnly(p.sso_only);
	}, [policyQuery.data]);

	const mutation = useMutation({
		meta: { success: "updated", subject: "loginSecurity" },
		mutationFn: () =>
			saveAuthSecurityPolicy({
				require_mfa: requireMfa,
				privileged_roles: allRoles ? [ALL_ROLES] : roles,
				ip_allow_list: parseList(ips),
				sso_only: ssoOnly,
			}),
		onSuccess: (policy) => {
			queryClient.setQueryData(authSecurityPolicyQueryOptions.queryKey, policy);
			setError(null);
		},
		onError: (err) =>
			setError(
				(
					{
						[ApiErrorCode.InvalidCidr]: t(
							"settings.security.errors.invalidCidr",
						),
						[ApiErrorCode.SelfLockout]: t(
							"settings.security.errors.selfLockout",
						),
						[ApiErrorCode.SsoOnlyNoProvider]: t(
							"settings.security.errors.ssoOnlyNoProvider",
						),
					} as Record<string, string>
				)[getApiErrorCode(err) ?? ""] ??
					t("settings.security.errors.saveFailed"),
			),
	});

	if (policyQuery.isPending) {
		return <Skeleton className="h-64 w-full rounded-xl" />;
	}
	if (policyQuery.isError) {
		return (
			<div className="flex flex-col items-start gap-2">
				<InlineNotice tone="error">
					{t("settings.security.errors.loadFailed")}
				</InlineNotice>
				<ReportIssueButton error={policyQuery.error} />
			</div>
		);
	}

	const policy = policyQuery.data;
	const roleNames = new Set((rolesQuery.data?.items ?? []).map((r) => r.name));
	// Keep roles already in the policy visible even if they were renamed or
	// fall outside the first page, so saving never drops one silently.
	const choices = [...new Set([...roleNames, ...roles])].sort((a, b) =>
		a.localeCompare(b),
	);
	const restricting = requireMfa || parseList(ips).length > 0;
	const savedRoles = policy.privileged_roles.includes(ALL_ROLES)
		? [ALL_ROLES]
		: policy.privileged_roles;

	const dirty =
		requireMfa !== policy.require_mfa ||
		(allRoles ? [ALL_ROLES] : roles).join("\n") !== savedRoles.join("\n") ||
		parseList(ips).join("\n") !== policy.ip_allow_list.join("\n") ||
		ssoOnly !== policy.sso_only;
	// Turning the mode on needs an enabled provider; turning it off never does.
	const noSsoProvider = ssoProvidersQuery.data?.length === 0;

	function toggleRole(name: string, on: boolean) {
		setRoles((cur) => (on ? [...cur, name] : cur.filter((r) => r !== name)));
	}

	return (
		<div className="flex flex-col gap-6">
			<section className="rounded-xl border border-border/60 bg-card p-6">
				<div className="flex items-start justify-between gap-4">
					<div>
						<h3 className="font-[Syne] text-base font-semibold">
							{t("settings.security.ssoOnly.title")}
						</h3>
						<p className="mt-0.5 text-sm text-muted-foreground">
							{t("settings.security.ssoOnly.description")}
						</p>
					</div>
					<Switch
						aria-label={t("settings.security.ssoOnly.title")}
						checked={ssoOnly}
						disabled={!ssoOnly && noSsoProvider}
						onCheckedChange={setSsoOnly}
					/>
				</div>
				{!ssoOnly && noSsoProvider ? (
					<p className="mt-3 text-xs text-muted-foreground">
						{t("settings.security.ssoOnly.noProvider")}
					</p>
				) : null}
				{ssoOnly ? (
					<InlineNotice tone="info" className="mt-4">
						{t("settings.security.ssoOnly.adminNote")}
					</InlineNotice>
				) : null}
			</section>

			<section className="rounded-xl border border-border/60 bg-card p-6">
				<h3 className="font-[Syne] text-base font-semibold">
					{t("settings.security.roles.title")}
				</h3>
				<p className="mt-0.5 mb-4 text-sm text-muted-foreground">
					{t("settings.security.roles.description")}
				</p>
				<label className="mb-3 flex cursor-pointer items-center gap-2 rounded-lg border border-border/60 px-3 py-2 text-sm font-medium hover:bg-muted/50">
					<Checkbox
						checked={allRoles}
						onCheckedChange={(value) => setAllRoles(value)}
					/>
					{t("settings.security.roles.all")}
				</label>
				{allRoles ? null : rolesQuery.isPending ? (
					<Skeleton className="h-20 w-full" />
				) : (
					<ul className="grid gap-2 sm:grid-cols-2">
						{choices.map((name) => (
							<li key={name}>
								<label className="flex cursor-pointer items-center gap-2 rounded-lg border border-border/60 px-3 py-2 text-sm hover:bg-muted/50">
									<Checkbox
										checked={roles.includes(name)}
										onCheckedChange={(value) => toggleRole(name, value)}
									/>
									<span className="truncate">{name}</span>
								</label>
							</li>
						))}
					</ul>
				)}
			</section>

			<section className="rounded-xl border border-border/60 bg-card p-6">
				<div className="flex items-start justify-between gap-4">
					<div>
						<h3 className="font-[Syne] text-base font-semibold">
							{t("settings.security.mfa.title")}
						</h3>
						<p className="mt-0.5 text-sm text-muted-foreground">
							{t("settings.security.mfa.description")}
						</p>
					</div>
					<Switch
						aria-label={t("settings.security.mfa.title")}
						checked={requireMfa}
						onCheckedChange={setRequireMfa}
					/>
				</div>
				{requireMfa ? (
					<InlineNotice tone="info" className="mt-4">
						{t("settings.security.mfa.rolloutNote")}
					</InlineNotice>
				) : null}
			</section>

			<section className="rounded-xl border border-border/60 bg-card p-6">
				<h3 className="font-[Syne] text-base font-semibold">
					{t("settings.security.ip.title")}
				</h3>
				<p className="mt-0.5 mb-4 text-sm text-muted-foreground">
					{t("settings.security.ip.description")}
				</p>
				<Label htmlFor="ip-allow-list" className="sr-only">
					{t("settings.security.ip.title")}
				</Label>
				<Textarea
					id="ip-allow-list"
					rows={4}
					className="font-mono text-sm"
					placeholder={"203.0.113.0/24\n2001:db8::/32"}
					value={ips}
					onChange={(e) => setIps(e.target.value)}
				/>
				<p className="mt-1.5 text-xs text-muted-foreground">
					{t("settings.security.ip.hint")}
				</p>
				{parseList(ips).length > 0 ? (
					<InlineNotice tone="warning" className="mt-3">
						{t("settings.security.ip.lockoutWarning")}
					</InlineNotice>
				) : null}
			</section>

			{restricting && !allRoles && roles.length === 0 ? (
				<InlineNotice tone="warning">
					{t("settings.security.noRoles")}
				</InlineNotice>
			) : null}
			{error ? <InlineNotice tone="error">{error}</InlineNotice> : null}

			<div className="flex items-center gap-2">
				<Button
					size="sm"
					disabled={!dirty || mutation.isPending}
					onClick={() => mutation.mutate()}
					className="gap-1.5"
				>
					{mutation.isPending ? (
						<Spinner aria-hidden="true" className="size-3.5" />
					) : null}
					{t("settings.general.save")}
				</Button>
			</div>
		</div>
	);
}
