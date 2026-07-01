import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogClose,
	DialogContent,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import type { InteractionView } from "@/lib/interaction-api";

interface RenameViewDialogProps {
	view: InteractionView | null;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onSubmit: (viewId: string, name: string) => Promise<unknown>;
	isPending?: boolean;
}

export function RenameViewDialog({
	view,
	open,
	onOpenChange,
	onSubmit,
	isPending,
}: RenameViewDialogProps) {
	const { t } = useTranslation();
	const [name, setName] = useState(view?.name ?? "");

	useEffect(() => {
		if (view) setName(view.name);
	}, [view]);

	const submit = async () => {
		if (!view || !name.trim()) return;
		await onSubmit(view.id, name.trim());
		onOpenChange(false);
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-xs">
				<DialogHeader>
					<DialogTitle>
						{t("project.interactions.layout.renameView")}
					</DialogTitle>
				</DialogHeader>
				<input
					value={name}
					onChange={(e) => setName(e.target.value)}
					onKeyDown={(e) => e.key === "Enter" && submit()}
					className="w-full rounded-lg border border-border/30 bg-muted/15 px-3.5 py-2.5 text-sm font-medium outline-none focus:border-primary/40 focus:ring-2 focus:ring-primary/15 placeholder:text-muted-foreground/50 transition-all duration-150"
				/>
				<DialogFooter>
					<DialogClose render={<Button variant="outline" size="sm" />}>
						{t("project.interactions.renameView.cancel")}
					</DialogClose>
					<Button
						size="sm"
						disabled={!name.trim() || isPending}
						onClick={submit}
					>
						{isPending
							? t("project.interactions.renameView.renaming")
							: t("common.rename")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
