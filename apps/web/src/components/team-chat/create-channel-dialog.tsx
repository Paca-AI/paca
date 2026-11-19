import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { type ChannelKind, chatKeys, createChannel } from "@/lib/chat-api";
import { chatErrorKey } from "@/lib/chat-errors";

export function CreateChannelDialog({
	projectId,
	open,
	onOpenChange,
}: {
	projectId: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const { t } = useTranslation("chat");
	const queryClient = useQueryClient();
	const navigate = useNavigate();
	const [name, setName] = useState("");
	const [topic, setTopic] = useState("");
	const [kind, setKind] = useState<ChannelKind>("public");
	const [error, setError] = useState<string | null>(null);

	const reset = () => {
		setName("");
		setTopic("");
		setKind("public");
		setError(null);
	};

	const create = useMutation({
		mutationFn: () =>
			createChannel(projectId, {
				kind,
				name: name.trim(),
				topic: topic.trim(),
			}),
		onSuccess: (c) => {
			void queryClient.invalidateQueries({
				queryKey: chatKeys.channels(projectId),
			});
			onOpenChange(false);
			reset();
			void navigate({
				to: "/projects/$projectId/chat/$channelId",
				params: { projectId, channelId: c.id },
			});
		},
		onError: (e) => setError(t(`errors.${chatErrorKey(e)}`)),
	});

	return (
		<Dialog
			open={open}
			onOpenChange={(o) => {
				onOpenChange(o);
				if (!o) reset();
			}}
		>
			<DialogContent className="sm:max-w-md">
				<form
					className="space-y-4"
					onSubmit={(e) => {
						e.preventDefault();
						if (name.trim()) create.mutate();
					}}
				>
					<DialogHeader>
						<DialogTitle>{t("create.title")}</DialogTitle>
						<DialogDescription>{t("create.description")}</DialogDescription>
					</DialogHeader>
					<div className="space-y-1.5">
						<Label htmlFor="chat-channel-name">{t("create.name")}</Label>
						<Input
							id="chat-channel-name"
							value={name}
							maxLength={80}
							placeholder={t("create.namePlaceholder")}
							onChange={(e) => setName(e.target.value)}
							autoFocus
						/>
					</div>
					<div className="space-y-1.5">
						<Label htmlFor="chat-channel-topic">{t("create.topic")}</Label>
						<Textarea
							id="chat-channel-topic"
							value={topic}
							rows={2}
							maxLength={250}
							placeholder={t("create.topicPlaceholder")}
							onChange={(e) => setTopic(e.target.value)}
						/>
					</div>
					<fieldset className="space-y-2">
						<legend className="text-sm font-medium">
							{t("create.visibility")}
						</legend>
						{(["public", "private"] as const).map((k) => (
							<label
								key={k}
								className="flex cursor-pointer items-start gap-2 text-sm"
							>
								<input
									type="radio"
									name="chat-channel-kind"
									className="mt-1"
									checked={kind === k}
									onChange={() => setKind(k)}
								/>
								<span>
									<span className="font-medium">{t(`create.${k}`)}</span>
									<span className="block text-xs text-muted-foreground">
										{t(`create.${k}Hint`)}
									</span>
								</span>
							</label>
						))}
					</fieldset>
					{error ? (
						<p role="alert" className="text-sm text-destructive">
							{error}
						</p>
					) : null}
					<DialogFooter>
						<Button
							type="button"
							variant="ghost"
							onClick={() => onOpenChange(false)}
						>
							{t("create.cancel")}
						</Button>
						<Button type="submit" disabled={!name.trim() || create.isPending}>
							{t("create.submit")}
						</Button>
					</DialogFooter>
				</form>
			</DialogContent>
		</Dialog>
	);
}
