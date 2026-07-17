// Where the SDD sensor dashboard lives.
//
// HARD-CODED ON PURPOSE (v1): the Paca plugin platform has no per-install
// config channel that reaches frontend components — the docs describe an
// InstalledPlugin.config object, but GET /api/v1/plugins returns only the
// typed manifest (services/api .../dto/plugin_dto.go) and the host forwards
// only extension-point props (projectId, ...) to remote components. Until the
// host grows a real config surface, changing the sensor URL means editing
// this constant and rebuilding (see ../../README.md "Changing the sensor
// URL"). Do NOT put secrets here: this file ships to every browser.
export const SDD_URL = "https://nexus.8verse.games/sdd-server";

/** Hostname shown in operator-facing fallback copy. */
export const SDD_HOST = new URL(SDD_URL).host;

/**
 * The origin that must be allowed to frame the sensor
 * (X-Frame-Options / CSP frame-ancestors on the SENSOR side).
 */
export const PACA_ORIGIN = "https://tasks.skyplatform.net";

/** How long we wait for the iframe load event before showing the fallback. */
export const LOAD_TIMEOUT_MS = 8000;
