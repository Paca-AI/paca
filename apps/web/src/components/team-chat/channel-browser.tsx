import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Hash, Lock, Plus, Volume2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { channelsQueryOptions, chatKeys, joinChannel } from "@/lib/chat-api";
import { CreateChannelDialog } from "./create-channel-dialog";

/** Every channel of the project the person may read, to open or follow. */
export function ChannelBrowser({ projectId }: { projectId: string }) {
	const { t } = useTranslation("chat");
	const queryClient = useQueryClient();
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const channels = useQuery(channelsQueryOptions(projectId));
	const [q, setQ] = useState("");
	const [creating, setCreating] = useState(false);
	const join = useMutation({
		mutationFn: (id: string) => joinChannel(projectId, id),
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: chatKeys.channels(projectId),
			});
			void queryClient.invalidateQueries({ queryKey: chatKeys.unread() });
		},
	});

	const list = (channels.data ?? [])
		.filter(
			(c) =>
				!c.archived_at && c.name.toLowerCase().includes(q.trim().toLowerCase()),
		)
		.sort((a, b) => a.name.localeCompare(b.name));

	return (
		<div className="mx-auto flex h-full min-h-0 w-full max-w-3xl flex-1 flex-col">
			<header className="flex items-center gap-2 border-b px-4 py-3">
				<div className="min-w-0 flex-1">
					<h1 className="text-base font-semibold">{t("browse.title")}</h1>
					<p className="text-xs text-muted-foreground">
						{t("browse.subtitle")}
					</p>
				</div>
				{hasProjectPermission("channels:write") ? (
					<Button size="sm" onClick={() => setCreating(true)}>
						<Plus />
						{t("channels.new")}
					</Button>
				) : null}
			</header>
			<div className="border-b p-3">
				<Input
					value={q}
					onChange={(e) => setQ(e.target.value)}
					placeholder={t("browse.search")}
					aria-label={t("browse.search")}
				/>
			</div>
			<div className="min-h-0 flex-1 overflow-y-auto">
				{channels.isPending ? (
					<div className="flex justify-center p-10">
						<Spinner />
					</div>
				) : list.length === 0 ? (
					<p className="p-10 text-center text-sm text-muted-foreground">
						{q ? t("browse.noMatch") : t("channels.empty")}
					</p>
				) : (
					<ul>
						{list.map((c) => (
							<li
								key={c.id}
								className="flex items-center gap-3 border-b px-4 py-3"
							>
								{c.kind === "private" ? (
									<Lock
										className="size-4 text-muted-foreground"
										aria-label={t("channels.private")}
									/>
								) : c.kind === "voice" ? (
									<Volume2
										className="size-4 text-muted-foreground"
										aria-label={t("channels.voice")}
									/>
								) : (
									<Hash
										className="size-4 text-muted-foreground"
										aria-label={t("channels.public")}
									/>
								)}
								<Link
									to="/projects/$projectId/chat/$channelId"
									params={{ projectId, channelId: c.id }}
									className="min-w-0 flex-1"
								>
									<span className="block truncate text-sm font-medium">
										{c.name}
									</span>
									<span className="block truncate text-xs text-muted-foreground">
										{c.topic || t("header.noTopic")}
									</span>
								</Link>
								{c.is_member ? (
									<span className="text-xs text-muted-foreground">
										{t("header.following")}
									</span>
								) : (
									<Button
										size="sm"
										variant="outline"
										disabled={join.isPending}
										onClick={() => join.mutate(c.id)}
									>
										{t("header.join")}
									</Button>
								)}
							</li>
						))}
					</ul>
				)}
			</div>
			<CreateChannelDialog
				projectId={projectId}
				open={creating}
				onOpenChange={setCreating}
			/>
		</div>
	);
}
