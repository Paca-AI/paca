// There's nothing left to configure here (see manifest.json — the content
// script runs everywhere and gates itself on the paca_port cookie) — this
// popup only reports whether the content script actually activated on the
// current tab. That comes from the background worker's per-tab state (see
// shared/messages.ts's SetActiveStateRequest/GetActiveStateRequest), not
// from checking the paca_port cookie directly here: that cookie is
// host-wide, so it's also present on the Paca app's own tabs, which are
// never themselves an activated preview.

import { getActiveState } from "../shared/messages";

const pill = document.getElementById("status-pill") as HTMLElement;

function setStatus(text: string, active: boolean): void {
	pill.textContent = text;
	pill.classList.toggle("inactive", !active);
}

async function checkActive(): Promise<void> {
	const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
	if (tab?.id === undefined) {
		setStatus("Not active on this page", false);
		return;
	}
	const active = await getActiveState(tab.id);
	setStatus(active ? "Active on this page" : "Not active on this page", active);
}

void checkActive();
