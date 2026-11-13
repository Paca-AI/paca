import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { KeyRound, Save, ServerCog, ShieldCheck, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { DeleteServiceAccountDialog } from "@/components/admin/service-accounts/DeleteServiceAccountDialog";
import { ServiceAccountKeysTab } from "@/components/admin/service-accounts/ServiceAccountKeysTab";
import { ServiceAccountRolesTab } from "@/components/admin/service-accounts/ServiceAccountRolesTab";
import { AvatarUpload } from "@/components/shared/avatar-upload";
import { NoPermissionState } from "@/components/shared/no-permission-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { usePermissions } from "@/hooks/use-permissions";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { formatDate } from "@/lib/format-date";
import { notifyError } from "@/lib/notify";
import { serviceAccountAvatar } from "@/lib/provider-logos";
import {
	type ServiceAccount,
	serviceAccountQueryOptions,
	serviceAccountsKey,
	updateServiceAccount,
} from "@/lib/service-account-api";

// The same shape as the agent detail page (components/projects/agents/
// agent-detail.tsx): a header with the avatar and name, an underline tab strip
// synced to the URL hash, and one scrolling pane per tab.

type Tab = "overview" | "api-keys" | "roles";

const TABS = [
	{
		id: "overview",
		labelKey: "serviceAccounts.detail.tabs.overview",
		icon: ServerCog,
	},
	{
		id: "api-keys",
		labelKey: "serviceAccounts.detail.tabs.apiKeys",
		icon: KeyRound,
	},
	{
		id: "roles",
		labelKey: "serviceAccounts.detail.tabs.roles",
		icon: ShieldCheck,
	},
] as const satisfies readonly { id: Tab; labelKey: string; icon: unknown }[];

const isTab = (value: string): value is Tab =>
	TABS.some((tab) => tab.id === value);

// ── Overview tab ──────────────────────────────────────────────────────────────

function OverviewTab({
	account,
	canWrite,
	onDelete,
}: {
	account: ServiceAccount;
	canWrite: boolean;
	onDelete: () => void;
}) {
	const { t } = useTranslation("admin");
	const queryClient = useQueryClient();
	const [name, setName] = useState(account.name);
	const [description, setDescription] = useState(account.description);

	const isDirty = name !== account.name || description !== account.description;

	const saveMutation = useMutation({
		meta: { success: "updated", subject: "serviceAccount" },
		onError: (err) => notifyError(err, saveErrorMessage(err)),
		mutationFn: () =>
			updateServiceAccount(account.id, {
				name: name.trim(),
				description: description.trim(),
			}),
		onSuccess: (updated) => {
			queryClient.setQueryData(
				serviceAccountQueryOptions(account.id).queryKey,
				updated,
			);
			void queryClient.invalidateQueries({
				queryKey: serviceAccountsKey,
			});
			setName(updated.name);
			setDescription(updated.description);
		},
	});

	const saveErrorMessage = (err: unknown) => {
		const code = getApiErrorCode(err);
		return code === ApiErrorCode.ServiceAccountNameInvalid
			? t("serviceAccounts.formDialog.errors.nameInvalid")
			: code === ApiErrorCode.ServiceAccountDescriptionTooLong
				? t("serviceAccounts.formDialog.errors.descriptionTooLong")
				: code === ApiErrorCode.ServiceAccountNotFound
					? t("serviceAccounts.formDialog.errors.notFound")
					: t("serviceAccounts.detail.overview.saveFailed");
	};
	const canSave =
		canWrite && isDirty && !!name.trim() && !saveMutation.isPending;

	const dateOptions = {
		year: "numeric",
		month: "short",
		day: "numeric",
	} as const;

	return (
		<div className="space-y-6 max-w-2xl">
			<div className="space-y-1.5">
				<Label htmlFor="sa-name">
					{t("serviceAccounts.formDialog.nameLabel")}
				</Label>
				<Input
					id="sa-name"
					value={name}
					maxLength={100}
					onChange={(e) => setName(e.target.value)}
					disabled={!canWrite}
				/>
			</div>

			<div className="space-y-1.5">
				<Label htmlFor="sa-description">
					{t("serviceAccounts.formDialog.descriptionLabel")}
				</Label>
				<Textarea
					id="sa-description"
					value={description}
					maxLength={1000}
					rows={3}
					onChange={(e) => setDescription(e.target.value)}
					disabled={!canWrite}
					placeholder={t("serviceAccounts.formDialog.descriptionPlaceholder")}
				/>
				<p className="text-xs text-muted-foreground">
					{t("serviceAccounts.detail.overview.descriptionHint")}
				</p>
			</div>

			{canWrite ? (
				<div className="flex items-center gap-3">
					<Button onClick={() => saveMutation.mutate()} disabled={!canSave}>
						{saveMutation.isPending ? (
							<Spinner aria-hidden="true" className="mr-2" />
						) : (
							<Save className="size-4 mr-2" />
						)}
						{t("serviceAccounts.detail.overview.saveChanges")}
					</Button>
				</div>
			) : null}

			<Separator />

			<dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
				<dt className="text-muted-foreground">
					{t("serviceAccounts.detail.overview.idLabel")}
				</dt>
				<dd className="select-all font-mono text-xs">{account.id}</dd>
				<dt className="text-muted-foreground">
					{t("serviceAccounts.detail.overview.createdLabel")}
				</dt>
				<dd>{formatDate(account.created_at, dateOptions)}</dd>
				<dt className="text-muted-foreground">
					{t("serviceAccounts.detail.overview.updatedLabel")}
				</dt>
				<dd>{formatDate(account.updated_at, dateOptions)}</dd>
			</dl>

			{canWrite ? (
				<>
					<Separator />
					<div className="flex items-center justify-between gap-4 rounded-xl border border-destructive/30 p-4">
						<div>
							<p className="text-sm font-medium">
								{t("serviceAccounts.detail.overview.deleteTitle")}
							</p>
							<p className="text-xs text-muted-foreground">
								{t("serviceAccounts.detail.overview.deleteHint")}
							</p>
						</div>
						<Button
							variant="outline"
							className="shrink-0 text-destructive hover:bg-destructive/10 hover:text-destructive"
							onClick={onDelete}
						>
							<Trash2 className="size-4 mr-1.5" />
							{t("serviceAccounts.deleteDialog.confirm")}
						</Button>
					</div>
				</>
			) : null}
		</div>
	);
}

// ── Page ──────────────────────────────────────────────────────────────────────

export function ServiceAccountDetailView({ accountId }: { accountId: string }) {
	const { t } = useTranslation("admin");
	const qc = useQueryClient();
	const navigate = useNavigate();
	const { hasPermission, isLoading: isPermissionsLoading } = usePermissions();
	const canRead = hasPermission("service_accounts:read");
	const canWrite = hasPermission("service_accounts:write");

	const {
		data: account,
		isLoading,
		error,
	} = useQuery({
		...serviceAccountQueryOptions(accountId),
		enabled: canRead,
		retry: false,
	});

	const [activeTab, setActiveTab] = useState<Tab>(() => {
		const hash = window.location.hash.slice(1);
		return isTab(hash) ? hash : "overview";
	});
	const [deleteOpen, setDeleteOpen] = useState(false);

	// Sync the tab when the hash changes (e.g. the back button).
	useEffect(() => {
		const handleHashChange = () => {
			const hash = window.location.hash.slice(1);
			if (isTab(hash)) setActiveTab(hash);
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

	if (!isPermissionsLoading && !canRead) {
		return (
			<div className="p-6">
				<NoPermissionState
					icon={ServerCog}
					title={t("serviceAccounts.noPermission.title")}
					description={t("serviceAccounts.noPermission.description")}
				/>
			</div>
		);
	}

	if (isLoading || isPermissionsLoading) {
		return (
			<div className="flex flex-col gap-4 p-6">
				<Skeleton className="h-16 w-full rounded-xl" />
				<Skeleton className="h-64 w-full rounded-xl" />
			</div>
		);
	}

	if (error || !account) {
		const notFound =
			getApiErrorCode(error) === ApiErrorCode.ServiceAccountNotFound;
		return (
			<div className="p-6">
				<NoPermissionState
					icon={ServerCog}
					title={
						notFound
							? t("serviceAccounts.detail.notFound.title")
							: t("serviceAccounts.errors.loadFailed")
					}
					description={
						notFound
							? t("serviceAccounts.detail.notFound.description")
							: t("serviceAccounts.errors.tryAgain")
					}
				/>
			</div>
		);
	}

	return (
		<div className="flex flex-col flex-1 min-h-0">
			{/* Header */}
			<div className="border-b border-border/50 px-6 py-5 shrink-0">
				<div className="flex items-center gap-4">
					<AvatarUpload
						basePath={`/admin/service-accounts/${account.id}`}
						avatar={{
							...serviceAccountAvatar(account),
							avatarUrl: account.avatar_url,
							size: "lg",
						}}
						disabled={!canWrite}
						className="size-12 rounded-xl"
						labels={{
							change: t("serviceAccounts.formDialog.avatar.change"),
							remove: t("serviceAccounts.formDialog.avatar.remove"),
							uploading: t("serviceAccounts.formDialog.avatar.uploading"),
							invalidType: t(
								"serviceAccounts.formDialog.avatar.errors.invalidType",
							),
							tooLarge: t("serviceAccounts.formDialog.avatar.errors.tooLarge"),
							uploadFailed: t(
								"serviceAccounts.formDialog.avatar.errors.uploadFailed",
							),
							removeFailed: t(
								"serviceAccounts.formDialog.avatar.errors.removeFailed",
							),
						}}
						onChange={(result) => {
							qc.setQueryData(
								serviceAccountQueryOptions(account.id).queryKey,
								(old) => (old ? { ...old, ...result } : old),
							);
							// The list, and the project member pickers, show the avatar too.
							void qc.invalidateQueries({
								queryKey: serviceAccountsKey,
							});
							void qc.invalidateQueries({
								predicate: (query) =>
									query.queryKey[0] === "projects" &&
									query.queryKey[2] === "members",
							});
						}}
					/>
					<div className="min-w-0">
						<h1 className="truncate text-lg font-semibold">{account.name}</h1>
						<div className="flex items-center gap-2 mt-0.5">
							<Badge variant="secondary" className="text-xs">
								{t("serviceAccounts.detail.badge")}
							</Badge>
							{account.description ? (
								<>
									<span className="text-muted-foreground/40">·</span>
									<span className="truncate text-sm text-muted-foreground">
										{account.description}
									</span>
								</>
							) : null}
						</div>
					</div>
				</div>
			</div>

			<Tabs
				value={activeTab}
				onValueChange={(v) => handleTabChange(v as Tab)}
				className="min-h-0 flex-1 gap-0"
			>
				<div className="border-b border-border/50 px-6 shrink-0 overflow-x-auto">
					<TabsList
						variant="line"
						className="h-10 w-max min-w-full justify-start"
					>
						{TABS.map((tab) => {
							const Icon = tab.icon;
							return (
								<TabsTrigger
									key={tab.id}
									value={tab.id}
									className="flex-none px-3"
								>
									<Icon />
									{t(tab.labelKey)}
								</TabsTrigger>
							);
						})}
					</TabsList>
				</div>

				<TabsContent value="overview" className="flex-1 overflow-auto p-6">
					<OverviewTab
						// Remount when the saved account changes so the form restarts from it.
						key={account.updated_at}
						account={account}
						canWrite={canWrite}
						onDelete={() => setDeleteOpen(true)}
					/>
				</TabsContent>
				<TabsContent value="api-keys" className="flex-1 overflow-auto p-6">
					<ServiceAccountKeysTab account={account} canWrite={canWrite} />
				</TabsContent>
				<TabsContent value="roles" className="flex-1 overflow-auto p-6">
					<ServiceAccountRolesTab account={account} canWrite={canWrite} />
				</TabsContent>
			</Tabs>

			{deleteOpen ? (
				<DeleteServiceAccountDialog
					account={account}
					open
					onDeleted={() => void navigate({ to: "/admin/service-accounts" })}
					onOpenChange={(open) => !open && setDeleteOpen(false)}
				/>
			) : null}
		</div>
	);
}
