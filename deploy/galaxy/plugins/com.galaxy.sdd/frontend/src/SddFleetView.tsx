import React from "react";
import SddFrame from "./SddFrame";
import { SDD_HOST, SDD_URL } from "./config";

/**
 * "SDD Fleet" — full-surface embed of the SDD sensor dashboard.
 *
 * Registered at two extension points with the same component:
 *  - "view":         selectable board layout (+ Add view → SDD Fleet); the
 *                    host renders it inside the interaction area and passes
 *                    {projectId, tasks, statuses, ...} — all ignored in v1.
 *  - "project.page": full-page route
 *                    /projects/:projectId/plugins/com.galaxy.sdd/sdd-fleet,
 *                    reached from the "SDD Fleet" sidebar nav item
 *                    (frontend.navItems) and from the sidebar card.
 *
 * Props are intentionally loose: the host forwards different prop bags per
 * surface and none of them are needed to render the iframe.
 */
export default function SddFleetView(_props: Record<string, unknown>) {
	return (
		<div
			style={{
				display: "flex",
				flexDirection: "column",
				flex: "1 1 0%",
				minHeight: 0,
				height: "100%",
				width: "100%",
			}}
		>
			<div
				style={{
					display: "flex",
					alignItems: "center",
					gap: 8,
					padding: "6px 12px",
					fontSize: 12,
					borderBottom: "1px solid var(--border, rgba(128,128,128,0.25))",
					color: "var(--muted-foreground, #888)",
					flex: "0 0 auto",
				}}
			>
				<span style={{ fontWeight: 600, color: "var(--foreground, inherit)" }}>
					SDD Fleet
				</span>
				<span style={{ opacity: 0.7 }}>{SDD_HOST}</span>
				<span style={{ flex: 1 }} />
				<a
					href={SDD_URL}
					target="_blank"
					rel="noreferrer noopener"
					style={{ color: "inherit" }}
				>
					Open in new tab
				</a>
			</div>
			<SddFrame />
		</div>
	);
}
