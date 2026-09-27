import { Check, Copy } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

/** A read-only value with a copy-to-clipboard button. */
export function CopyableValue({ id, value }: { id?: string; value: string }) {
	const { t } = useTranslation("admin");
	const [copied, setCopied] = useState(false);

	return (
		<div className="flex gap-2">
			<Input
				id={id}
				readOnly
				value={value}
				className="font-mono text-xs"
				onFocus={(e) => e.currentTarget.select()}
			/>
			<Button
				type="button"
				variant="outline"
				size="icon"
				aria-label={t("settings.sso.copy")}
				onClick={() => {
					void navigator.clipboard?.writeText(value).then(() => {
						setCopied(true);
						setTimeout(() => setCopied(false), 1500);
					});
				}}
			>
				{copied ? <Check className="size-4" /> : <Copy className="size-4" />}
			</Button>
		</div>
	);
}
