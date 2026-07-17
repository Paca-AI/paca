import React from "react";
import { SDD_HOST, SDD_URL } from "./config";

/**
 * Small project-sidebar card ("sidebar.project.section").
 *
 * The host renders it with componentProps = { projectId } only (see
 * app-sidebar.tsx) — no SDK object arrives at runtime, so links are plain
 * anchors. The in-app link targets the routed full page contributed by this
 * plugin's navItems ("project.page" / slug sdd-fleet); a plain href is a full
 * document load, which is deliberate: the host router is not exposed to
 * plugins and half-working pushState hacks are worse than a clean reload.
 */
export default function SddSidebarCard(props: { projectId?: string }) {
	const projectId =
		typeof props.projectId === "string" && props.projectId.length > 0
			? props.projectId
			: null;

	const linkStyle: React.CSSProperties = {
		color: "inherit",
		fontWeight: 600,
		textDecoration: "none",
		borderBottom: "1px dotted currentColor",
	};

	return (
		<div style={{ padding: "4px 12px" }}>
			<div
				style={{
					border: "1px solid var(--sidebar-border, rgba(128,128,128,0.25))",
					borderRadius: 8,
					padding: "10px 12px",
					fontSize: 12,
					lineHeight: 1.5,
					color: "var(--sidebar-foreground, inherit)",
				}}
			>
				<div style={{ fontWeight: 600, marginBottom: 2 }}>SDD Sensor</div>
				<div style={{ opacity: 0.7, marginBottom: 8 }}>
					Fleet dashboard from {SDD_HOST}
				</div>
				<div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
					{projectId ? (
						<a
							href={`/projects/${encodeURIComponent(projectId)}/plugins/com.galaxy.sdd/sdd-fleet`}
							style={linkStyle}
						>
							SDD Fleet
						</a>
					) : null}
					<a
						href={SDD_URL}
						target="_blank"
						rel="noreferrer noopener"
						style={{ ...linkStyle, fontWeight: 400, opacity: 0.85 }}
					>
						Open sensor
					</a>
				</div>
			</div>
		</div>
	);
}
