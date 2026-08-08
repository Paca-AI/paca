import { useQuery } from "@tanstack/react-query";
import { brandingQueryOptions } from "@/lib/settings-api";

/** Instance-wide branding (logo/favicon/primary colors), set from the admin
 * settings page. Backed by the public GET /branding endpoint, so this is
 * safe to call from unauthenticated pages (e.g. the login screen). */
export function useBranding() {
	const { data } = useQuery(brandingQueryOptions);
	return data;
}
