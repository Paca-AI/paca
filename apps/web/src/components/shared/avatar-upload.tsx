import { Camera, X } from "lucide-react";
import { useRef, useState } from "react";
import {
	EntityAvatar,
	type EntityAvatarProps,
} from "@/components/shared/entity-avatar";
import { Spinner } from "@/components/ui/spinner";
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
	/** What to render: the owner's identity plus its uploaded `avatarUrl`
	 * (never a placeholder default — the remove button keys off it). */
	avatar: EntityAvatarProps;
	onChange: (result: AvatarResult) => void;
	labels: AvatarUploadLabels;
	/** Sizing/shape for the avatar itself, e.g. "size-14 rounded-xl". */
	className?: string;
	disabled?: boolean;
}

export function AvatarUpload({
	basePath,
	avatar,
	onChange,
	labels,
	className,
	disabled,
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
				<EntityAvatar
					{...avatar}
					shape="inherit"
					className={cn("size-full", avatar.className)}
				/>

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
						<Spinner aria-hidden="true" />
					) : (
						<Camera className="size-4" />
					)}
				</button>

				{avatar.avatarUrl && !uploading ? (
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
