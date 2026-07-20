import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useMemo } from "react";
import { type PluginRegistration, pluginsQueryOptions } from "@/lib/plugin-api";
import { createPluginApiClient } from "./plugin-client";

/**
 * Host-side implementation of BaseExtensionProps (@paca-ai/plugin-sdk-react)
 * — every mounted plugin component destructures `{ api, ui, meta }` from its
 * props, but RemoteComponent only forwards whatever componentProps its
 * caller passes in, so each route that mounts a plugin page must build this
 * itself. `toast`/`confirm` are stubs — the host has no toast or confirm
 * dialog system yet — `navigate` is wired to the real router since that's
 * already available.
 *
 * `registration` may be undefined while the caller's own data is still
 * resolving — call this unconditionally (before any early return) like any
 * other hook, the result just won't be used until a real registration is
 * available.
 */
export function usePluginBaseProps(
	registration: PluginRegistration | undefined,
	projectId?: string,
) {
	const navigate = useNavigate();
	const { data: plugins = [] } = useQuery(pluginsQueryOptions);
	const pluginId = registration?.pluginId ?? "";
	const pluginName = registration?.pluginName ?? "";
	const version =
		plugins.find((p) => p.manifest.id === pluginId)?.manifest.version ??
		"0.0.0";

	return useMemo(
		() => ({
			api: createPluginApiClient(projectId),
			ui: {
				toast: () => {},
				confirm: () => Promise.resolve(false),
				navigate: (path: string) => void navigate({ to: path as "/" }),
			},
			meta: { pluginId, displayName: pluginName, version },
		}),
		[pluginId, pluginName, projectId, version, navigate],
	);
}
