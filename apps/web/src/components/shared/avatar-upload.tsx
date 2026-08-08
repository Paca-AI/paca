import { Camera, Loader2, X } from "lucide-react";
import { type ReactNode, useRef, useState } from "react";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import {
	ACCEPTED_AVATAR_CONTENT_TYPES,
	type AvatarResult,
	removeAvatar,
	uploadAvatar,
	validateAvatarFile,
} from "@/lib/avatar-api";
import { cn } from "@/lib/utils";

export interface AvatarUploadLabels {
	change: string;
	remove: string;
	uploading: string;
	invalidType: string;
	tooLarge: string;
	uploadFailed: string;
	removeFailed: string;
}

interface AvatarUploadProps {
	/** Owner path — "/users/me", "/projects/{id}/agents/{id}", or "/admin/agents/{id}". */
	basePath: string;
	avatarUrl?: string | null;
	/** Shown when there's no avatar (initials or an icon). */
	fallback: ReactNode;
	onChange: (result: AvatarResult) => void;
	labels: AvatarUploadLabels;
	/** Sizing/shape for the avatar itself, e.g. "size-14 rounded-xl". */
	className?: string;
	fallbackClassName?: string;
	disabled?: boolean;
	/** Whether the remove button can show at all — pass `false` when
	 * `avatarUrl` is a placeholder (e.g. a provider-logo default) rather than
	 * a real upload, since there's nothing to remove. Defaults to `true`. */
	canRemove?: boolean;
}

export function AvatarUpload({
	basePath,
	avatarUrl,
	fallback,
	onChange,
	labels,
	className,
	fallbackClassName,
	disabled,
	canRemove = true,
}: AvatarUploadProps) {
	const inputRef = useRef<HTMLInputElement>(null);
	const [uploading, setUploading] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const handleFileChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
		const file = e.target.files?.[0];
		e.target.value = "";
		if (!file) return;

		const validationError = validateAvatarFile(file);
		if (validationError) {
			setError(
				validationError === "tooLarge" ? labels.tooLarge : labels.invalidType,
			);
			return;
		}

		setError(null);
		setUploading(true);
		try {
			const result = await uploadAvatar(basePath, file);
			onChange(result);
		} catch {
			setError(labels.uploadFailed);
		} finally {
			setUploading(false);
		}
	};

	const handleRemove = async () => {
		setError(null);
		setUploading(true);
		try {
			const result = await removeAvatar(basePath);
			onChange(result);
		} catch {
			setError(labels.removeFailed);
		} finally {
			setUploading(false);
		}
	};

	return (
		<div className="flex flex-col gap-1.5">
			{/* The shape (e.g. "rounded-xl") lives here, on the outer frame, so the
			    photo, the initials fallback, and the hover overlay can all inherit
			    the exact same radius via `rounded-[inherit]` instead of each
			    needing their own copy of it. */}
			<div
				className={cn(
					"relative inline-flex size-8 shrink-0 rounded-full group/avatar-upload",
					className,
				)}
			>
				<Avatar className="size-full rounded-[inherit] after:rounded-[inherit]">
					{/* Always mounted (never conditionally omitted): base-ui's Avatar
					    tracks image-load status internally and only shows Fallback
					    once it observes a missing/failed src — omitting this element
					    from the tree instead leaves that internal state stuck at
					    whatever it last was, so removing an avatar left the whole
					    thing blank (no photo, no fallback) until a full reload. */}
					<AvatarImage
						src={avatarUrl ?? undefined}
						className="rounded-[inherit]"
					/>
					<AvatarFallback
						className={cn("rounded-[inherit]", fallbackClassName)}
					>
						{fallback}
					</AvatarFallback>
				</Avatar>

				<button
					type="button"
					aria-label={labels.change}
					title={labels.change}
					disabled={disabled || uploading}
					onClick={() => inputRef.current?.click()}
					className={cn(
						"absolute inset-0 flex items-center justify-center rounded-[inherit] bg-black/50 text-white opacity-0 transition-opacity group-hover/avatar-upload:opacity-100 disabled:cursor-not-allowed",
						uploading && "opacity-100",
					)}
				>
					{uploading ? (
						<Loader2 className="size-4 animate-spin" />
					) : (
						<Camera className="size-4" />
					)}
				</button>

				{avatarUrl && canRemove && !uploading ? (
					<button
						type="button"
						aria-label={labels.remove}
						title={labels.remove}
						disabled={disabled}
						onClick={handleRemove}
						className="absolute -top-1 -right-1 flex size-5 items-center justify-center rounded-full bg-muted text-muted-foreground ring-2 ring-background hover:bg-muted/80"
					>
						<X className="size-3" />
					</button>
				) : null}

				<input
					ref={inputRef}
					type="file"
					accept={ACCEPTED_AVATAR_CONTENT_TYPES.join(",")}
					className="hidden"
					disabled={disabled || uploading}
					onChange={handleFileChange}
				/>
			</div>
			{error ? <p className="text-xs text-destructive">{error}</p> : null}
		</div>
	);
}
