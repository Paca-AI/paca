import { FileCode2, Lock, LockOpen } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import type { DocSource } from "@/lib/doc-api";

/** First sentence of the banner: which repository file this document copies.
 *  Also the tooltip of the managed-copy marker in the docs tree. */
export function useManagedCopyLabel(): (source: DocSource) => string {
	const { t } = useTranslation("projects");
	return (source) =>
		source.commit
			? t("docs.managedCopy.copyOfAtCommit", {
					path: source.path,
					repository: source.repository,
					commit: source.commit.slice(0, 7),
				})
			: t("docs.managedCopy.copyOf", {
					path: source.path,
					repository: source.repository,
				});
}

interface ManagedCopyBannerProps {
	/** The document's source; the banner renders nothing without one. */
	source: DocSource | undefined;
	locked: boolean;
	/** Only members who may write docs get the Lock / Unlock button. */
	canWrite: boolean;
	/** Called with the lock state to change to. */
	onToggleLock: (locked: boolean) => void;
	/** True while a lock change is in flight. */
	pending?: boolean;
}

/**
 * Shown above a document that an agent pushes from a repository file: says
 * where it comes from and whether edits made in Paca are allowed. The page
 * owns the lock request; `locked` is always the state the server last sent.
 */
export function ManagedCopyBanner({
	source,
	locked,
	canWrite,
	onToggleLock,
	pending = false,
}: ManagedCopyBannerProps) {
	const { t } = useTranslation("projects");
	const label = useManagedCopyLabel();
	if (!source) return null;

	const StateIcon = locked ? Lock : FileCode2;
	return (
		<section
			aria-label={label(source)}
			className="mb-4 flex items-start gap-3 rounded-lg border border-border/40 bg-muted/30 px-4 py-3 text-sm"
		>
			<StateIcon
				aria-hidden
				className="mt-0.5 size-4 shrink-0 text-muted-foreground"
			/>
			<div className="min-w-0 flex-1 space-y-0.5">
				<p className="break-words font-medium text-foreground/90">
					{label(source)}
				</p>
				<p className="text-muted-foreground">
					{locked
						? t("docs.managedCopy.locked")
						: t("docs.managedCopy.unlocked")}
				</p>
			</div>
			{canWrite && (
				<Button
					type="button"
					variant="outline"
					size="sm"
					className="shrink-0"
					disabled={pending}
					onClick={() => onToggleLock(!locked)}
				>
					{locked ? (
						<LockOpen aria-hidden className="size-3.5" />
					) : (
						<Lock aria-hidden className="size-3.5" />
					)}
					{locked ? t("docs.managedCopy.unlock") : t("docs.managedCopy.lock")}
				</Button>
			)}
		</section>
	);
}
