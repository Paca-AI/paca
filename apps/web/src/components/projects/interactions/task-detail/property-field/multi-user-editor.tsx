import { Check, Sparkles } from "lucide-react";
import { useTranslation } from "react-i18next";
import { EntityAvatarContent } from "@/components/shared/entity-avatar";
import { FieldValue } from "../primitives";
import { ChipField } from "./chip-field";
import type { UserOption } from "./types";

// Sentinel chip key for the "Auto" row — namespaced with underscores so it
// can never collide with a real user id (a UUID).
const AUTO_CHIP_KEY = "__auto__";

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

function AutoListButton({
	label,
	isSelected,
	onClick,
}: {
	label: string;
	isSelected: boolean;
	onClick: () => void;
}) {
	return (
		<button
			type="button"
			className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm hover:bg-muted/60 transition-colors duration-100"
			onClick={onClick}
		>
			<div className="flex size-5 items-center justify-center rounded-full bg-linear-to-br from-primary/20 to-primary/10 text-primary shrink-0">
				<Sparkles className="size-3" />
			</div>
			<span className="flex-1 text-left truncate">{label}</span>
			{isSelected && <Check className="size-3.5 text-primary" />}
		</button>
	);
}

/** Optional "Auto" row folded into the assignee popover/chip list — selecting
 * it hands assignment off to Jev (see assignment_mode on the task) instead of
 * a specific user. Represented as a special AUTO_CHIP_KEY-keyed chip so it
 * reuses ChipField's native remove-X affordance to toggle back to manual. */
interface AutoOption {
	isSelected: boolean;
	label: string;
	onToggle: () => void;
}

export function MultiUserEditor({
	userValues = [],
	users = [],
	canEdit,
	onChange,
	autoOption,
}: {
	userValues?: UserOption[];
	users?: UserOption[];
	canEdit: boolean;
	onChange?: (values: string[]) => void;
	autoOption?: AutoOption;
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

	function removeChip(key: string) {
		if (key === AUTO_CHIP_KEY) {
			autoOption?.onToggle();
			return;
		}
		toggle(key);
	}

	if (!canEdit) {
		if (autoOption?.isSelected) {
			return (
				<span className="inline-flex items-center gap-1.5 rounded-full border border-primary/30 bg-primary/10 px-2.5 py-0.5 text-xs font-semibold text-primary">
					<Sparkles className="size-3" />
					{autoOption.label}
				</span>
			);
		}
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
			chips={[
				...(autoOption?.isSelected
					? [
							{
								key: AUTO_CHIP_KEY,
								label: (
									<span className="inline-flex items-center gap-1.5">
										<Sparkles className="size-3 text-primary" />
										{autoOption.label}
									</span>
								),
							},
						]
					: []),
				...userValues.map((u) => ({
					key: u.value,
					label: (
						<span className="inline-flex items-center gap-1.5">
							<UserAvatar initials={u.initials} avatarUrl={u.avatarUrl} />
							{u.label}
						</span>
					),
				})),
			]}
			onRemoveChip={removeChip}
			canEdit={canEdit}
			addLabel={t("taskDetail.propertyField.multiSelectEditor.addOption")}
		>
			{autoOption && (
				<AutoListButton
					label={autoOption.label}
					isSelected={autoOption.isSelected}
					onClick={autoOption.onToggle}
				/>
			)}
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
