import React from "react";

// Dependency-free inline SVG icons (Lucide-style 24×24 stroke paths). Stateless
// functional components with NO hooks — safe across React copies, exactly like
// com.galaxy.analytics's TipView/DistributionLegend. We do NOT depend on
// lucide-react (keeps the bundle tiny and the plugin dependency-light).

export type IconName =
	| "server"
	| "users"
	| "folder"
	| "activity"
	| "shield"
	| "lock"
	| "list"
	| "radar"
	| "git"
	| "files"
	| "refresh"
	| "circle"
	| "alert"
	| "check"
	| "user"
	| "clock"
	| "layers"
	| "gauge"
	| "globe";

// Each entry is the inner markup of a 24×24 viewBox stroke icon.
const PATHS: Record<IconName, React.ReactNode> = {
	server: (
		<>
			<rect x="2" y="3" width="20" height="8" rx="2" />
			<rect x="2" y="13" width="20" height="8" rx="2" />
			<path d="M6 7h.01M6 17h.01" />
		</>
	),
	users: (
		<>
			<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
			<circle cx="9" cy="7" r="4" />
			<path d="M22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75" />
		</>
	),
	folder: <path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13c0 1.1.9 2 2 2Z" />,
	activity: <path d="M22 12h-4l-3 9L9 3l-3 9H2" />,
	shield: (
		<>
			<path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1Z" />
			<path d="M12 8v4M12 16h.01" />
		</>
	),
	lock: (
		<>
			<rect x="3" y="11" width="18" height="11" rx="2" />
			<path d="M7 11V7a5 5 0 0 1 10 0v4" />
		</>
	),
	list: (
		<>
			<path d="M3 6h.01M3 12h.01M3 18h.01" />
			<path d="M8 6h13M8 12h13M8 18h13" />
		</>
	),
	radar: (
		<>
			<path d="M19.07 4.93A10 10 0 0 0 6.99 3.34" />
			<path d="M4 6h.01M2.29 9.62a10 10 0 1 0 18.4-2.87" />
			<path d="M12 12l4-4M12 12a2 2 0 1 0 0 .01" />
		</>
	),
	git: (
		<>
			<circle cx="12" cy="18" r="3" />
			<circle cx="6" cy="6" r="3" />
			<circle cx="18" cy="6" r="3" />
			<path d="M18 9v1a2 2 0 0 1-2 2H8a2 2 0 0 0-2 2v1M12 12v3" />
		</>
	),
	files: (
		<>
			<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z" />
			<path d="M14 2v5h5" />
		</>
	),
	refresh: (
		<>
			<path d="M3 12a9 9 0 0 1 15-6.7L21 8" />
			<path d="M21 3v5h-5M21 12a9 9 0 0 1-15 6.7L3 16" />
			<path d="M3 21v-5h5" />
		</>
	),
	circle: <circle cx="12" cy="12" r="6" />,
	alert: (
		<>
			<circle cx="12" cy="12" r="10" />
			<path d="M12 8v4M12 16h.01" />
		</>
	),
	check: (
		<>
			<circle cx="12" cy="12" r="10" />
			<path d="m9 12 2 2 4-4" />
		</>
	),
	user: (
		<>
			<path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2" />
			<circle cx="12" cy="7" r="4" />
		</>
	),
	clock: (
		<>
			<circle cx="12" cy="12" r="10" />
			<path d="M12 6v6l4 2" />
		</>
	),
	layers: <path d="m12 2 9 5-9 5-9-5 9-5ZM3 12l9 5 9-5M3 17l9 5 9-5" />,
	gauge: (
		<>
			<path d="m12 14 4-4" />
			<path d="M3.34 19a10 10 0 1 1 17.32 0" />
		</>
	),
	globe: (
		<>
			<circle cx="12" cy="12" r="10" />
			<path d="M2 12h20M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10Z" />
		</>
	),
};

interface IconProps {
	name: IconName;
	size?: number;
	className?: string;
	/** Fill (solid) icons like a status dot use fill instead of stroke. */
	fill?: boolean;
	style?: React.CSSProperties;
}

export function Icon(props: IconProps) {
	const size = props.size ?? 16;
	return (
		<svg
			width={size}
			height={size}
			viewBox="0 0 24 24"
			fill={props.fill ? "currentColor" : "none"}
			stroke={props.fill ? "none" : "currentColor"}
			strokeWidth={2}
			strokeLinecap="round"
			strokeLinejoin="round"
			className={props.className}
			style={{ flex: "0 0 auto", ...props.style }}
			aria-hidden="true"
		>
			{PATHS[props.name]}
		</svg>
	);
}
