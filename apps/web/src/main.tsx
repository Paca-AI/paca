import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import "./i18n/config";
import { consumeDockSsoRelayHash } from "./components/app-shell/galaxy-dock";
import { QueryProvider } from "./integrations/react-query/root-provider";
import { router } from "./router";

// Galaxy chat dock SSO relay return leg (ADR-038 P3.2): consume and scrub a
// #dock_sso= fragment before the app renders so the token never lingers in
// the URL bar or router state.
consumeDockSsoRelayHash();

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("Root element not found");
createRoot(rootElement).render(
	<StrictMode>
		<QueryProvider>
			<RouterProvider router={router} />
		</QueryProvider>
	</StrictMode>,
);
