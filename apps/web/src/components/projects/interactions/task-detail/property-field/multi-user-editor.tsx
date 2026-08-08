import { Check } from "lucide-react";
import { useTranslation } from "react-i18next";
import { EntityAvatarContent } from "@/components/shared/entity-avatar";
import { FieldValue } from "../primitives";
import { ChipField } from "./chip-field";
import type { UserOption } from "./types";

function UserAvatar({
	initials,
	avatarUrl,
}: {
	initials: string;
	avatarUrl?: string | null;
}) {
	return (
		<div className="flex size-5 items-center justify-center rounded-full bg-linear-to-br from-primary/20 to-primary/10 text-primary text-xs font-bold shrink-0">
			<EntityAvatarContent avatarUrl={avatarUrl}>
				{initials}
			</EntityAvatarContent>
		</div>
	);
}

function UserListButton({
	user,
	isSelected,
	onClick,
}: {
	user: UserOption;
	isSelected: boolean;
	onClick: () => void;
}) {
	return (
		<button
			type="button"
			className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm hover:bg-muted/60 transition-colors duration-100"
			onClick={onClick}
		>
			<UserAvatar initials={user.initials} avatarUrl={user.avatarUrl} />
			<span className="flex-1 text-left truncate">{user.label}</span>
			{isSelected && <Check className="size-3.5 text-primary" />}
		</button>
	);
}

export function MultiUserEditor({
	userValues = [],
	users = [],
	canEdit,
	onChange,
}: {
	userValues?: UserOption[];
	users?: UserOption[];
	canEdit: boolean;
	onChange?: (values: string[]) => void;
}) {
	const { t } = useTranslation("projects");
	const selectedIds = userValues.map((u) => u.value);

	function toggle(userId: string) {
		onChange?.(
			selectedIds.includes(userId)
				? selectedIds.filter((v) => v !== userId)
				: [...selectedIds, userId],
		);
	}

	if (!canEdit) {
		if (userValues.length === 0) return <FieldValue empty />;
		return (
			<div className="flex flex-wrap items-center gap-1.5">
				{userValues.map((u) => (
					<span
						key={u.value}
						className="inline-flex items-center gap-1.5 rounded-full border border-border/30 bg-muted/30 px-2.5 py-0.5 text-xs font-semibold text-muted-foreground"
					>
						<UserAvatar initials={u.initials} avatarUrl={u.avatarUrl} />
						{u.label}
					</span>
				))}
			</div>
		);
	}

	return (
		<ChipField
			chips={userValues.map((u) => ({
				key: u.value,
				label: (
					<span className="inline-flex items-center gap-1.5">
						<UserAvatar initials={u.initials} avatarUrl={u.avatarUrl} />
						{u.label}
					</span>
				),
			}))}
			onRemoveChip={toggle}
			canEdit={canEdit}
			addLabel={t("taskDetail.propertyField.multiSelectEditor.addOption")}
		>
			{users.map((u) => (
				<UserListButton
					key={u.value}
					user={u}
					isSelected={selectedIds.includes(u.value)}
					onClick={() => toggle(u.value)}
				/>
			))}
		</ChipField>
	);
}
