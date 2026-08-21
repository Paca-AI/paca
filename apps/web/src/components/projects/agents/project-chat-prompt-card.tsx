import { Check } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

export interface ProjectChatPromptOption {
	value: string;
	label: ReactNode;
}

export interface ProjectChatPromptChoiceGroup {
	id: string;
	options: ProjectChatPromptOption[];
	selectedValues: string[];
	multiple?: boolean;
	onSelectedValuesChange: (values: string[]) => void;
}

export function ProjectChatPromptCard({
	groups,
	input,
	error,
	pending,
	confirmDisabled,
	cancelLabel,
	confirmLabel,
	onCancel,
	onConfirm,
}: {
	groups?: ProjectChatPromptChoiceGroup[];
	input?: {
		value: string;
		placeholder: string;
		onFocus?: () => void;
		onChange: (value: string) => void;
	};
	error?: string | null;
	pending?: boolean;
	confirmDisabled?: boolean;
	cancelLabel: string;
	confirmLabel: string;
	onCancel: () => void;
	onConfirm: () => void;
}) {
	return (
		<div className="mb-2 rounded-xl border border-border/60 bg-background/95 p-3 shadow-sm backdrop-blur-sm">
			<div className="space-y-2.5">
				{groups?.map((group) => (
					<fieldset key={group.id} disabled={pending} className="space-y-1.5">
						{group.options.map((option) => {
							const selected = group.selectedValues.includes(option.value);
							return (
								<button
									type="button"
									key={option.value}
									aria-pressed={selected}
									disabled={pending}
									onClick={() => {
										if (group.multiple) {
											group.onSelectedValuesChange(
												selected
													? group.selectedValues.filter(
															(value) => value !== option.value,
														)
													: [...group.selectedValues, option.value],
											);
											return;
										}
										group.onSelectedValuesChange([option.value]);
									}}
									className={cn(
										"flex min-h-9 w-full items-center gap-2 rounded-lg border px-3 py-2 text-left text-sm transition-colors disabled:opacity-50",
										selected
											? "border-primary/40 bg-primary/8 text-foreground"
											: "border-border/60 text-muted-foreground hover:bg-muted/50 hover:text-foreground",
									)}
								>
									{selected && <Check className="size-3.5 text-primary" />}
									{option.label}
								</button>
							);
						})}
					</fieldset>
				))}
				{input && (
					<Textarea
						value={input.value}
						onFocus={input.onFocus}
						onChange={(event) => input.onChange(event.target.value)}
						placeholder={input.placeholder}
						disabled={pending}
						className="min-h-16 resize-none"
						aria-label={input.placeholder}
					/>
				)}
				{error && <p className="text-sm text-destructive">{error}</p>}
			</div>
			<div className="mt-3 flex justify-end gap-2">
				<Button
					type="button"
					variant="ghost"
					size="sm"
					onClick={onCancel}
					disabled={pending}
				>
					{cancelLabel}
				</Button>
				<Button
					type="button"
					size="sm"
					onClick={onConfirm}
					disabled={pending || confirmDisabled}
				>
					{confirmLabel}
				</Button>
			</div>
		</div>
	);
}
