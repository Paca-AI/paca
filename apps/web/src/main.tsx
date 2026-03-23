import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import { QueryProvider } from "./integrations/react-query/root-provider";
import { router } from "./router";

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("Root element not found");
createRoot(rootElement).render(
	<StrictMode>
		<QueryProvider>
			<RouterProvider router={router} />
		</QueryProvider>
	</StrictMode>,
);
