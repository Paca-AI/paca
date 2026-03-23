import { useEffect, useState } from "react";

export function useIsDark(): boolean {
	const [isDark, setIsDark] = useState(() =>
		document.documentElement.classList.contains("dark"),
	);

	useEffect(() => {
		const root = document.documentElement;
		const sync = () => setIsDark(root.classList.contains("dark"));

		const observer = new MutationObserver(sync);
		observer.observe(root, { attributes: true, attributeFilter: ["class"] });

		return () => observer.disconnect();
	}, []);

	return isDark;
}
