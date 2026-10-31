import qrcode from "qrcode-generator";
import { useMemo } from "react";

/** Renders `value` as an SVG QR code. Drawn dark-on-white whatever the theme,
 * since scanners need the contrast. */
export function QrCode({ value, label }: { value: string; label: string }) {
	const svg = useMemo(() => {
		const qr = qrcode(0, "M");
		qr.addData(value);
		qr.make();
		return qr.createSvgTag({ cellSize: 4, margin: 0, scalable: true });
	}, [value]);

	return (
		<div
			role="img"
			aria-label={label}
			className="size-44 shrink-0 rounded-lg border border-border/60 bg-white p-3 [&>svg]:size-full"
			// biome-ignore lint/security/noDangerouslySetInnerHtml: SVG produced locally by qrcode-generator from the otpauth URI, never from user markup
			dangerouslySetInnerHTML={{ __html: svg }}
		/>
	);
}
