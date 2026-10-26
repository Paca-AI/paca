import { CheckIcon, MessageCircleQuestionIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { type AskQuestion, formatAskAnswer } from "@/lib/ask-question";
import { cn } from "@/lib/utils";

interface AskQuestionCardProps {
	ask: AskQuestion;
	/** True once the question has been answered (or can no longer be). */
	disabled?: boolean;
	/** Called with the formatted answer text; may reject to allow a retry. */
	onAnswer: (answer: string) => void | Promise<void>;
	className?: string;
}

/** A multiple-choice question from an agent, with a free-text escape hatch. */
export function AskQuestionCard({
	ask,
	disabled = false,
	onAnswer,
	className,
}: AskQuestionCardProps) {
	const { t } = useTranslation("shared");
	const [selected, setSelected] = useState<string[]>([]);
	const [other, setOther] = useState("");
	const [submitted, setSubmitted] = useState<string | null>(null);

	const locked = disabled || submitted !== null;

	const submit = async (answers: string[]) => {
		const text = formatAskAnswer(answers.filter((a) => a.trim()));
		if (!text || locked) return;
		setSubmitted(text);
		try {
			await onAnswer(text);
		} catch {
			setSubmitted(null);
		}
	};

	const toggle = (label: string) =>
		setSelected((prev) =>
			prev.includes(label) ? prev.filter((l) => l !== label) : [...prev, label],
		);

	const otherAnswer = other.trim();

	return (
		<div
			data-slot="ask-question-card"
			className={cn(
				"flex flex-col gap-2 rounded-md border border-border/50 bg-muted/30 p-3 text-sm",
				className,
			)}
		>
			<p className="flex items-start gap-2 font-medium">
				<MessageCircleQuestionIcon className="mt-0.5 size-4 shrink-0 text-primary" />
				<span className="whitespace-pre-wrap">{ask.question}</span>
			</p>
			<div className="flex flex-col gap-1.5">
				{ask.options.map((option) => {
					const isSelected = selected.includes(option.label);
					return (
						<Button
							key={option.label}
							type="button"
							size="sm"
							variant={isSelected ? "default" : "outline"}
							disabled={locked}
							aria-pressed={ask.multiSelect ? isSelected : undefined}
							className="h-auto items-start justify-start gap-2 py-1.5 text-start whitespace-normal active:scale-[0.99]"
							onClick={() =>
								ask.multiSelect
									? toggle(option.label)
									: void submit([option.label])
							}
						>
							{ask.multiSelect && (
								<CheckIcon
									className={cn(
										"mt-0.5 size-3.5 shrink-0",
										!isSelected && "opacity-0",
									)}
								/>
							)}
							<span className="flex flex-col">
								<span>{option.label}</span>
								{option.description && (
									<span className="text-xs font-normal opacity-70">
										{option.description}
									</span>
								)}
							</span>
						</Button>
					);
				})}
			</div>
			<form
				className="flex items-center gap-2"
				onSubmit={(e) => {
					e.preventDefault();
					void submit([...selected, otherAnswer]);
				}}
			>
				<Input
					value={other}
					onChange={(e) => setOther(e.target.value)}
					disabled={locked}
					placeholder={t("askQuestion.otherPlaceholder")}
					className="h-8 text-xs"
				/>
				<Button
					type="submit"
					size="sm"
					disabled={locked || (selected.length === 0 && !otherAnswer)}
					className="shrink-0"
				>
					{t("askQuestion.submit")}
				</Button>
			</form>
			{submitted !== null && (
				<p className="text-muted-foreground text-xs">
					{t("askQuestion.answered", { answer: submitted })}
				</p>
			)}
		</div>
	);
}
