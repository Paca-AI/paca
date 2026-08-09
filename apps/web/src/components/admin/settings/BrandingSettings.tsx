import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Image as ImageIcon, Loader2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { AvatarUpload } from "@/components/shared/avatar-upload";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { AvatarResult } from "@/lib/avatar-api";
import {
	brandingQueryOptions,
	setBrandingQueryData,
	updateSettings,
} from "@/lib/settings-api";

// A curated set of light/dark accent-color pairs, rather than a free-form
// color picker — keeps every option pre-vetted for contrast (foreground
// text color is still auto-computed at render time, see branding-effects.tsx)
// and avoids admins landing on something illegible.
const COLOR_PRESETS = [
	{ key: "green", light: "#5a9e1c", dark: "#9ed957" },
	{ key: "blue", light: "#2563eb", dark: "#60a5fa" },
	{ key: "teal", light: "#0d9488", dark: "#2dd4bf" },
	{ key: "indigo", light: "#4f46e5", dark: "#818cf8" },
	{ key: "purple", light: "#7c3aed", dark: "#a78bfa" },
	{ key: "pink", light: "#db2777", dark: "#f472b6" },
	{ key: "red", light: "#dc2626", dark: "#f87171" },
	{ key: "orange", light: "#ea580c", dark: "#fb923c" },
] as const;

export function BrandingSettings() {
	const { t } = useTranslation("admin");
	const queryClient = useQueryClient();
	const { data: branding } = useQuery(brandingQueryOptions);

	const [brandName, setBrandName] = useState<string | null>(
		branding?.brand_name ?? null,
	);
	const [colorLight, setColorLight] = useState<string | null>(
		branding?.primary_color_light ?? null,
	);
	const [colorDark, setColorDark] = useState<string | null>(
		branding?.primary_color_dark ?? null,
	);
	const [generalError, setGeneralError] = useState<string | null>(null);
	const [saved, setSaved] = useState(false);

	const mutation = useMutation({
		mutationFn: () =>
			updateSettings({
				brand_name: brandName,
				primary_color_light: colorLight,
				primary_color_dark: colorDark,
			}),
		onSuccess: (updated) => {
			setBrandingQueryData(queryClient, (old) =>
				old ? { ...old, ...updated } : updated,
			);
			setGeneralError(null);
			setSaved(true);
			setTimeout(() => setSaved(false), 2500);
		},
		onError: () => {
			setGeneralError(t("settings.general.errors.updateFailed"));
		},
	});

	const isDirty =
		brandName !== (branding?.brand_name ?? null) ||
		colorLight !== (branding?.primary_color_light ?? null) ||
		colorDark !== (branding?.primary_color_dark ?? null);

	// AvatarUpload's onChange delivers {avatar_url, avatar_thumb_url} — the
	// generic shape the backend's logo/favicon endpoints deliberately mirror
	// (see settings_dto.go's AvatarShapedImageResponse) so this component can
	// drive both through the existing avatar-upload client unmodified. Map
	// that back onto the branding cache's logo_*/favicon_* fields here.
	function updateImageCache(slot: "logo" | "favicon", result: AvatarResult) {
		setBrandingQueryData(queryClient, (old) =>
			old
				? slot === "logo"
					? {
							...old,
							logo_url: result.avatar_url,
							logo_thumb_url: result.avatar_thumb_url,
						}
					: {
							...old,
							favicon_url: result.avatar_url,
							favicon_thumb_url: result.avatar_thumb_url,
						}
				: old,
		);
	}

	return (
		<div className="flex flex-col gap-6">
			<div className="rounded-xl border border-border/60 bg-card p-6">
				<h3 className="font-[Syne] text-base font-semibold mb-4">
					{t("settings.images.title")}
				</h3>
				<div className="flex flex-wrap gap-10">
					<div className="flex flex-col items-center gap-2">
						<AvatarUpload
							basePath="/admin/settings/logo"
							avatarUrl={branding?.logo_url}
							fallback={<ImageIcon className="size-5" />}
							className="size-16 rounded-xl"
							fallbackClassName="bg-muted text-muted-foreground"
							labels={{
								change: t("settings.images.logo.change"),
								remove: t("settings.images.logo.remove"),
								uploading: t("settings.images.logo.uploading"),
								invalidType: t("settings.images.errors.invalidType"),
								tooLarge: t("settings.images.errors.tooLarge"),
								uploadFailed: t("settings.images.errors.uploadFailed"),
								removeFailed: t("settings.images.errors.removeFailed"),
							}}
							onChange={(result) => updateImageCache("logo", result)}
						/>
						<span className="text-xs font-medium text-muted-foreground">
							{t("settings.images.logo.label")}
						</span>
					</div>

					<div className="flex flex-col items-center gap-2">
						<AvatarUpload
							basePath="/admin/settings/favicon"
							avatarUrl={branding?.favicon_url}
							fallback={<ImageIcon className="size-4" />}
							className="size-16 rounded-xl"
							fallbackClassName="bg-muted text-muted-foreground"
							labels={{
								change: t("settings.images.favicon.change"),
								remove: t("settings.images.favicon.remove"),
								uploading: t("settings.images.favicon.uploading"),
								invalidType: t("settings.images.errors.invalidType"),
								tooLarge: t("settings.images.errors.tooLarge"),
								uploadFailed: t("settings.images.errors.uploadFailed"),
								removeFailed: t("settings.images.errors.removeFailed"),
							}}
							onChange={(result) => updateImageCache("favicon", result)}
						/>
						<span className="text-xs font-medium text-muted-foreground">
							{t("settings.images.favicon.label")}
						</span>
					</div>
				</div>
			</div>

			<div className="rounded-xl border border-border/60 bg-card p-6">
				<h3 className="font-[Syne] text-base font-semibold mb-4">
					{t("settings.general.title")}
				</h3>

				<div className="space-y-1.5 mb-6 max-w-sm">
					<Label htmlFor="brand-name">
						{t("settings.general.brandNameLabel")}
					</Label>
					<Input
						id="brand-name"
						value={brandName ?? ""}
						onChange={(e) => setBrandName(e.target.value)}
						placeholder={t("settings.general.brandNamePlaceholder")}
					/>
				</div>

				<p className="text-sm font-medium mb-1">
					{t("settings.general.colorsLabel")}
				</p>
				<p className="text-sm text-muted-foreground mb-4">
					{t("settings.general.colorsDescription")}
				</p>

				<div className="flex flex-wrap gap-3">
					{COLOR_PRESETS.map((preset) => {
						const selected =
							colorLight === preset.light && colorDark === preset.dark;
						return (
							<button
								key={preset.key}
								type="button"
								aria-pressed={selected}
								onClick={() => {
									setColorLight(preset.light);
									setColorDark(preset.dark);
								}}
								className={`flex flex-col items-center gap-1.5 rounded-lg p-1.5 transition-colors ${
									selected
										? "ring-2 ring-primary ring-offset-2 ring-offset-card"
										: "hover:bg-muted/50"
								}`}
							>
								<span
									className="size-9 rounded-full border border-border/60"
									style={{
										background: `linear-gradient(135deg, ${preset.light} 50%, ${preset.dark} 50%)`,
									}}
								/>
								<span className="text-xs text-muted-foreground">
									{t(`settings.general.colorPresets.${preset.key}`)}
								</span>
							</button>
						);
					})}
				</div>

				{generalError ? (
					<p className="mt-3 text-xs text-destructive bg-destructive/10 rounded-lg px-3 py-2">
						{generalError}
					</p>
				) : null}

				<div className="flex items-center gap-2 pt-4">
					<Button
						size="sm"
						disabled={!isDirty || mutation.isPending}
						onClick={() => mutation.mutate()}
						className="gap-1.5"
					>
						{mutation.isPending ? (
							<Loader2 className="size-3.5 animate-spin" />
						) : null}
						{t("settings.general.save")}
					</Button>
					{saved ? (
						<span className="text-xs text-emerald-600 dark:text-emerald-400 font-medium">
							{t("settings.general.saved")}
						</span>
					) : null}
				</div>
			</div>
		</div>
	);
}
