import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	ArrowUpCircle,
	Database,
	Download,
	ExternalLink,
	LayoutTemplate,
	Search,
	Server,
	Trash2,
	Zap,
} from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
	installMarketplacePlugin,
	type MarketplacePlugin,
	marketplacePluginsQueryOptions,
	pluginsQueryOptions,
	uninstallPlugin,
	upgradePlugin,
} from "@/lib/plugin-api";

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
	onInstall: (name: string) => void;
	onUninstall: (name: string) => void;
	onUpgrade: (pluginName: string) => void;
}) {
	const { t } = useTranslation();
	const { artifacts } = plugin;
	const hasBackend = !!artifacts.backend_tar_gz_url;
	const hasFrontend = !!artifacts.frontend_tar_gz_url;
	const hasMigrations = !!artifacts.migrations_tar_gz_url;
	const hasMCP = !!artifacts.mcp_tar_gz_url;

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
							<Badge className="text-xs">{t("admin.plugins.installed")}</Badge>
						) : null}
						{upgradeAvailable ? (
							<Badge variant="secondary" className="text-xs gap-1">
								<ArrowUpCircle className="size-3" />
								{t("admin.plugins.updateAvailable")}
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
							label={t("admin.plugins.featBackend")}
						/>
					)}
					{hasFrontend && (
						<FeatureBadge
							icon={<LayoutTemplate className="size-3" />}
							label={t("admin.plugins.featFrontend")}
						/>
					)}
					{hasMigrations && (
						<FeatureBadge
							icon={<Database className="size-3" />}
							label={t("admin.plugins.featMigrations")}
						/>
					)}
					{hasMCP && (
						<FeatureBadge
							icon={<Zap className="size-3" />}
							label={t("admin.plugins.featMcp")}
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
							{t("admin.plugins.source")}
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
										? t("admin.plugins.upgrading")
										: t("admin.plugins.upgradeTo", {
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
									? t("admin.plugins.uninstalling")
									: t("admin.plugins.uninstall")}
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
								? t("admin.plugins.installing")
								: t("admin.plugins.install")}
						</Button>
					)}
				</div>
			</div>
		</div>
	);
}

export function PluginMarketplacePanel() {
	const { t } = useTranslation();
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

	const installMutation = useMutation({
		mutationFn: installMarketplacePlugin,
		onSuccess: async () => {
			await Promise.all([
				qc.invalidateQueries({ queryKey: ["plugins"] }),
				qc.invalidateQueries({ queryKey: ["plugins", "marketplace"] }),
			]);
		},
	});

	const uninstallMutation = useMutation({
		mutationFn: uninstallPlugin,
		onSuccess: async () => {
			await Promise.all([
				qc.invalidateQueries({ queryKey: ["plugins"] }),
				qc.invalidateQueries({ queryKey: ["plugins", "marketplace"] }),
			]);
		},
	});

	const upgradeMutation = useMutation({
		mutationFn: upgradePlugin,
		onSuccess: async () => {
			await qc.invalidateQueries({ queryKey: ["plugins"] });
		},
	});

	if (isLoading) {
		return (
			<div className="text-sm text-muted-foreground py-6">
				{t("admin.plugins.loadingMarketplace")}
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
					placeholder={t("admin.plugins.searchPlaceholder")}
					className="pl-9"
				/>
			</div>

			{filtered.length === 0 ? (
				<div className="text-sm text-muted-foreground py-6">
					{t("admin.plugins.noPlugins")}
				</div>
			) : (
				<div className="grid grid-cols-1 gap-3">
					{filtered.map((plugin) => (
						<PluginCard
							key={plugin.name}
							plugin={plugin}
							isInstalled={installedByName.has(plugin.name)}
							installedVersion={installedByName.get(plugin.name)?.version}
							isInstalling={
								installMutation.isPending &&
								installMutation.variables?.name === plugin.name
							}
							isUninstalling={
								uninstallMutation.isPending &&
								uninstallMutation.variables ===
									installedByName.get(plugin.name)?.id
							}
							isUpgrading={
								upgradeMutation.isPending &&
								upgradeMutation.variables ===
									installedByName.get(plugin.name)?.id
							}
							onInstall={(name) =>
								installMutation.mutate({ name, enabled: true })
							}
							onUninstall={(name) => {
								const pluginId = installedByName.get(name)?.id;
								if (!pluginId) return;
								uninstallMutation.mutate(pluginId);
							}}
							onUpgrade={(name) => {
								const pluginId = installedByName.get(name)?.id;
								if (!pluginId) return;
								upgradeMutation.mutate(pluginId);
							}}
						/>
					))}
				</div>
			)}
		</div>
	);
}
