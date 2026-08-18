import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import { i18nReady } from "./i18n/config";
import { queryClient } from "./integrations/react-query/query-client";
import { QueryProvider } from "./integrations/react-query/root-provider";
import { currentUserQueryOptions } from "./lib/auth-api";
import { brandingQueryOptions } from "./lib/settings-api";
import { router } from "./router";

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("Root element not found");

// Kick off "who am I" and "branding" as early as physically possible. Both
// are needed for the very first render — the former by _authenticated's
// beforeLoad guard, the latter by BrandingEffects at the root — but without
// this they don't start until the router actually reaches that point in its
// (partly serial) match/load sequence. Firing them here lets them run fully
// in parallel with each other, with i18n loading, and with route matching.
// React Query dedupes each against whatever fetches it again later, so this
// is pure overlap, never a duplicate request.
void queryClient.prefetchQuery(currentUserQueryOptions);
void queryClient.prefetchQuery(brandingQueryOptions);

// Waits for the active language's translations (see i18n/config.ts) so the
// app never mounts with untranslated/raw keys. Resolves immediately for the
// "en" default since that locale ships eagerly.
void i18nReady.then(() => {
	createRoot(rootElement).render(
		<StrictMode>
			<QueryProvider>
				<RouterProvider router={router} />
			</QueryProvider>
		</StrictMode>,
	);
});
