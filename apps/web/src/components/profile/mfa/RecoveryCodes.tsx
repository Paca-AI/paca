import { Check, Copy, Download } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { copyToClipboard } from "@/lib/clipboard";

/** Shows one-time recovery codes with copy and download actions. */
export function RecoveryCodes({ codes }: { codes: string[] }) {
	const { t } = useTranslation("profile");
	const [copied, setCopied] = useState(false);

	const text = codes.join("\n");

	async function copy() {
		if (await copyToClipboard(text)) {
			setCopied(true);
			setTimeout(() => setCopied(false), 2000);
		}
	}

	function download() {
		const url = URL.createObjectURL(
			new Blob([`${text}\n`], { type: "text/plain" }),
		);
		const a = document.createElement("a");
		a.href = url;
		a.download = "paca-recovery-codes.txt";
		a.click();
		URL.revokeObjectURL(url);
	}

	return (
		<div className="space-y-3">
			<ul className="grid grid-cols-2 gap-2 rounded-lg border border-border/60 bg-muted/40 p-3 font-mono text-sm">
				{codes.map((code) => (
					<li key={code} className="select-all text-center tracking-wider">
						{code}
					</li>
				))}
			</ul>
			<div className="flex flex-wrap gap-2">
				<Button type="button" size="sm" variant="outline" onClick={copy}>
					{copied ? <Check className="size-4" /> : <Copy className="size-4" />}
					{copied
						? t("security.mfa.codes.copied")
						: t("security.mfa.codes.copy")}
				</Button>
				<Button type="button" size="sm" variant="outline" onClick={download}>
					<Download className="size-4" />
					{t("security.mfa.codes.download")}
				</Button>
			</div>
		</div>
	);
}
