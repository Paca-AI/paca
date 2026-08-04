import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import {
	AlertTriangle,
	ArrowUpCircle,
	Database,
	Download,
	ExternalLink,
	LayoutTemplate,
	Search,
	Server,
	Trash2,
	Wand2,
	Zap,
} from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
	ApiErrorCode,
	getApiErrorCode,
	getApiErrorDetails,
	getApiErrorMessage,
} from "@/lib/api-error";
import {
	installMarketplacePlugin,
	type MarketplacePlugin,
	marketplacePluginsQueryOptions,
	pluginsQueryOptions,
	uninstallPlugin,
	upgradePlugin,
} from "@/lib/plugin-api";

/**
 * Resolves an install/upgrade mutation failure to a fully translated message
 * worth showing the admin. `PLUGIN_INCOMPATIBLE_HOST_VERSION` carries
 * request-specific detail (the required and running versions) as structured,
 * non-localized `error_details` — interpolated into a translated string here
 * rather than showing the server's English-only `error` text directly. Other
 * recognized codes get a plain friendly translated message; anything else
 * falls back to the raw server message, then a generic translated string.
 */
function resolveMutationErrorMessage(
	err: unknown,
	t: TFunction<"plugins">,
): string {
	const code = getApiErrorCode(err);

	if (code === ApiErrorCode.PluginIncompatibleHostVersion) {
		const details = getApiErrorDetails(err);
		if (details?.required_version && details?.host_version) {
			return t("marketplace.card.errors.incompatibleHostVersion", {
				required: details.required_version,
				current: details.host_version,
			});
		}
		return t("marketplace.card.errors.incompatibleHostVersionGeneric");
	}

	const knownMessages: Partial<Record<string, string>> = {
		[ApiErrorCode.PluginAlreadyUpToDate]: t(
			"marketplace.card.errors.alreadyUpToDate",
		),
		[ApiErrorCode.PluginDowngradeNotAllowed]: t(
			"marketplace.card.errors.downgradeNotAllowed",
		),
		[ApiErrorCode.PluginNotFound]: t("marketplace.card.errors.notFound"),
		[ApiErrorCode.PluginNameTaken]: t("marketplace.card.errors.nameTaken"),
	};
	return (
		(code && knownMessages[code]) ??
		getApiErrorMessage(err) ??
		t("marketplace.card.errors.generic")
	);
}

function initials(name: string): string {
	const words = name
		.trim()
		.split(/\s+/)
		.filter(Boolean)
		.slice(0, 2)
		.map((v) => v[0]?.toUpperCase() ?? "");
	return words.join("") || "PL";
}

function matchesQuery(plugin: MarketplacePlugin, query: string): boolean {
	if (!query) return true;
	const q = query.toLowerCase();
	return (
		plugin.name.toLowerCase().includes(q) ||
		plugin.display_name.toLowerCase().includes(q) ||
		plugin.description.toLowerCase().includes(q)
	);
}

/**
 * Returns >0 if a is newer, 0 if equal, <0 if a is older.
 * Only strict "X.Y.Z" (or "vX.Y.Z") versions are accepted.
 * Pre-release identifiers and build metadata cause an error.
 */
function compareSemver(a: string, b: string): number {
	const parse = (v: string): [number, number, number] => {
		v = v.replace(/^v/, "");
		// Reject build metadata
		if (v.includes("+")) {
			throw new Error(
				`Version "${v}" must not contain build metadata; only strict X.Y.Z versions are supported`,
			);
		}
		// Reject pre-release identifiers
		if (v.includes("-")) {
			throw new Error(
				`Version "${v}" must not contain pre-release identifiers; only strict X.Y.Z versions are supported`,
			);
		}
		const parts = v.split(".");
		if (parts.length !== 3) {
			throw new Error(`Expected major.minor.patch, got "${v}"`);
		}
		const nums = parts.map((p) => {
			const n = Number.parseInt(p, 10);
			if (Number.isNaN(n) || n < 0) {
				throw new Error(`Non-numeric version component "${p}" in "${v}"`);
			}
			return n;
		});
		return [nums[0], nums[1], nums[2]];
	};
	const pa = parse(a);
	const pb = parse(b);
	for (let i = 0; i < 3; i++) {
		const diff = pa[i] - pb[i];
		if (diff !== 0) return diff;
	}
	return 0;
}

interface FeatureBadgeProps {
	icon: React.ReactNode;
	label: string;
}

function FeatureBadge({ icon, label }: FeatureBadgeProps) {
	return (
		<Badge variant="secondary" className="gap-1.5 text-xs h-5">
			{icon}
			<span>{label}</span>
		</Badge>
	);
}

function PluginCard({
	plugin,
	isInstalled,
	isInstalling,
	isUninstalling,
	isUpgrading,
	installedVersion,
	errorMessage,
	onInstall,
	onUninstall,
	onUpgrade,
}: {
	plugin: MarketplacePlugin;
	isInstalled: boolean;
	isInstalling: boolean;
	isUninstalling: boolean;
	isUpgrading: boolean;
	installedVersion?: string;
	errorMessage?: string;
	onInstall: (name: string) => void;
	onUninstall: (name: string) => void;
	onUpgrade: (pluginName: string) => void;
}) {
	const { t } = useTranslation("plugins");
	const { artifacts } = plugin;
	const hasBackend = !!artifacts.backend_tar_gz_url;
	const hasFrontend = !!artifacts.frontend_tar_gz_url;
	const hasMigrations = !!artifacts.migrations_tar_gz_url;
	const hasMCP = !!artifacts.mcp_tar_gz_url;
	const hasSkills = !!artifacts.skills_tar_gz_url;

	let upgradeAvailable = false;
	if (isInstalled && installedVersion) {
		try {
			upgradeAvailable = compareSemver(plugin.version, installedVersion) > 0;
		} catch (err) {
			// Invalid version format - treat as no upgrade available
			console.warn(
				`Invalid version format for plugin ${plugin.name}: marketplace=${plugin.version}, installed=${installedVersion}`,
				err,
			);
		}
	}

	return (
		<div className="rounded-lg border border-border/60 bg-card p-4 space-y-3 hover:border-border/80 transition-colors">
			<div className="flex items-start gap-3">
				<Avatar size="lg">
					<AvatarImage src={plugin.avatar_url} alt={plugin.display_name} />
					<AvatarFallback>{initials(plugin.display_name)}</AvatarFallback>
				</Avatar>
				<div className="min-w-0 flex-1">
					<div className="flex items-center gap-2 flex-wrap">
						<p className="font-medium text-sm truncate">
							{plugin.display_name}
						</p>
						<Badge variant="outline" className="text-xs">
							{plugin.version}
						</Badge>
						{isInstalled ? (
							<Badge className="text-xs">
								{t("marketplace.card.installed")}
							</Badge>
						) : null}
						{upgradeAvailable ? (
							<Badge variant="secondary" className="text-xs gap-1">
								<ArrowUpCircle className="size-3" />
								{t("marketplace.card.updateAvailable")}
							</Badge>
						) : null}
					</div>
					<p className="text-xs text-muted-foreground truncate">
						{plugin.name}
					</p>
				</div>
			</div>

			<div className="rounded-md bg-muted/40 px-3 py-2">
				<p className="text-xs whitespace-pre-wrap leading-5">
					{plugin.description}
				</p>
			</div>

			<div className="space-y-2">
				<div className="flex items-center gap-1.5 flex-wrap">
					{hasBackend && (
						<FeatureBadge
							icon={<Server className="size-3" />}
							label={t("marketplace.card.features.backend")}
						/>
					)}
					{hasFrontend && (
						<FeatureBadge
							icon={<LayoutTemplate className="size-3" />}
							label={t("marketplace.card.features.frontend")}
						/>
					)}
					{hasMigrations && (
						<FeatureBadge
							icon={<Database className="size-3" />}
							label={t("marketplace.card.features.migrations")}
						/>
					)}
					{hasMCP && (
						<FeatureBadge
							icon={<Zap className="size-3" />}
							label={t("marketplace.card.features.mcp")}
						/>
					)}
					{hasSkills && (
						<FeatureBadge
							icon={<Wand2 className="size-3" />}
							label={t("marketplace.card.features.skills")}
						/>
					)}
				</div>

				<div className="flex items-center justify-between gap-2">
					{plugin.repository_url ? (
						<a
							href={plugin.repository_url}
							target="_blank"
							rel="noreferrer"
							className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
						>
							<ExternalLink className="size-3.5" />
							{t("marketplace.card.source")}
						</a>
					) : (
						<span /> // Spacer for alignment
					)}
					{isInstalled ? (
						<div className="flex items-center gap-2">
							{upgradeAvailable ? (
								<Button
									size="sm"
									variant="secondary"
									disabled={isUpgrading || isUninstalling}
									onClick={() => onUpgrade(plugin.name)}
								>
									<ArrowUpCircle className="size-4" />
									{isUpgrading
										? t("marketplace.card.upgrading")
										: t("marketplace.card.upgradeTo", {
												version: plugin.version,
											})}
								</Button>
							) : null}
							<Button
								size="sm"
								variant="destructive"
								disabled={isUninstalling || isUpgrading}
								onClick={() => onUninstall(plugin.name)}
							>
								<Trash2 className="size-4" />
								{isUninstalling
									? t("marketplace.card.uninstalling")
									: t("marketplace.card.uninstall")}
							</Button>
						</div>
					) : (
						<Button
							size="sm"
							disabled={isInstalling}
							onClick={() => onInstall(plugin.name)}
						>
							<Download className="size-4" />
							{isInstalling
								? t("marketplace.card.installing")
								: t("marketplace.card.install")}
						</Button>
					)}
				</div>

				{errorMessage ? (
					<div className="flex items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
						<AlertTriangle className="size-3.5 shrink-0 translate-y-0.5" />
						<span>{errorMessage}</span>
					</div>
				) : null}
			</div>
		</div>
	);
}

export function PluginMarketplacePanel() {
	const { t } = useTranslation("plugins");
	const qc = useQueryClient();
	const [query, setQuery] = useState("");

	const { data: marketplace = [], isLoading } = useQuery(
		marketplacePluginsQueryOptions,
	);
	const { data: installed = [] } = useQuery(pluginsQueryOptions);

	const installedByName = useMemo(() => {
		return new Map(installed.map((p) => [p.name, p]));
	}, [installed]);

	const filtered = useMemo(() => {
		return marketplace.filter((plugin) => matchesQuery(plugin, query));
	}, [marketplace, query]);

	const [installingNames, setInstallingNames] = useState<Set<string>>(
		new Set(),
	);
	const [uninstallingIds, setUninstallingIds] = useState<Set<string>>(
		new Set(),
	);
	const [upgradingIds, setUpgradingIds] = useState<Set<string>>(new Set());

	// Keyed by marketplace plugin name / installed plugin id respectively, so
	// each card shows only the error from its own most recent attempt.
	const [installErrors, setInstallErrors] = useState<Map<string, string>>(
		new Map(),
	);
	const [upgradeErrors, setUpgradeErrors] = useState<Map<string, string>>(
		new Map(),
	);

	const installMutation = useMutation({
		mutationFn: installMarketplacePlugin,
		onMutate: (variables) => {
			setInstallingNames((prev) => new Set(prev).add(variables.name));
			setInstallErrors((prev) => {
				if (!prev.has(variables.name)) return prev;
				const next = new Map(prev);
				next.delete(variables.name);
				return next;
			});
		},
		onSettled: (_data, _error, variables) => {
			setInstallingNames((prev) => {
				const next = new Set(prev);
				next.delete(variables.name);
				return next;
			});
		},
		onError: (err, variables) => {
			const message = resolveMutationErrorMessage(err, t);
			setInstallErrors((prev) => new Map(prev).set(variables.name, message));
		},
		onSuccess: async () => {
			await Promise.all([
				qc.invalidateQueries({ queryKey: ["plugins"] }),
				qc.invalidateQueries({ queryKey: ["plugins", "marketplace"] }),
			]);
		},
	});

	const uninstallMutation = useMutation({
		mutationFn: uninstallPlugin,
		onMutate: (pluginId) => {
			setUninstallingIds((prev) => new Set(prev).add(pluginId));
		},
		onSettled: (_data, _error, pluginId) => {
			setUninstallingIds((prev) => {
				const next = new Set(prev);
				next.delete(pluginId);
				return next;
			});
		},
		onSuccess: async () => {
			await Promise.all([
				qc.invalidateQueries({ queryKey: ["plugins"] }),
				qc.invalidateQueries({ queryKey: ["plugins", "marketplace"] }),
			]);
		},
	});

	const upgradeMutation = useMutation({
		mutationFn: upgradePlugin,
		onMutate: (pluginId) => {
			setUpgradingIds((prev) => new Set(prev).add(pluginId));
			setUpgradeErrors((prev) => {
				if (!prev.has(pluginId)) return prev;
				const next = new Map(prev);
				next.delete(pluginId);
				return next;
			});
		},
		onSettled: (_data, _error, pluginId) => {
			setUpgradingIds((prev) => {
				const next = new Set(prev);
				next.delete(pluginId);
				return next;
			});
		},
		onError: (err, pluginId) => {
			const message = resolveMutationErrorMessage(err, t);
			setUpgradeErrors((prev) => new Map(prev).set(pluginId, message));
		},
		onSuccess: async () => {
			await qc.invalidateQueries({ queryKey: ["plugins"] });
		},
	});

	if (isLoading) {
		return (
			<div className="text-sm text-muted-foreground py-6">
				{t("marketplace.loading")}
			</div>
		);
	}

	return (
		<div className="space-y-4">
			<div className="relative">
				<Search className="size-4 text-muted-foreground absolute left-3 top-1/2 -translate-y-1/2" />
				<Input
					value={query}
					onChange={(e) => setQuery(e.target.value)}
					placeholder={t("marketplace.searchPlaceholder")}
					className="pl-9"
				/>
			</div>

			{filtered.length === 0 ? (
				<div className="text-sm text-muted-foreground py-6">
					{t("marketplace.empty")}
				</div>
			) : (
				<div className="grid grid-cols-1 gap-3">
					{filtered.map((plugin) => {
						const pluginId = installedByName.get(plugin.name)?.id;
						return (
							<PluginCard
								key={plugin.name}
								plugin={plugin}
								isInstalled={installedByName.has(plugin.name)}
								installedVersion={installedByName.get(plugin.name)?.version}
								isInstalling={installingNames.has(plugin.name)}
								isUninstalling={!!pluginId && uninstallingIds.has(pluginId)}
								isUpgrading={!!pluginId && upgradingIds.has(pluginId)}
								errorMessage={
									installErrors.get(plugin.name) ??
									(pluginId ? upgradeErrors.get(pluginId) : undefined)
								}
								onInstall={(name) =>
									installMutation.mutate({ name, enabled: true })
								}
								onUninstall={() => {
									if (!pluginId) return;
									uninstallMutation.mutate(pluginId);
								}}
								onUpgrade={() => {
									if (!pluginId) return;
									upgradeMutation.mutate(pluginId);
								}}
							/>
						);
					})}
				</div>
			)}
		</div>
	);
}
