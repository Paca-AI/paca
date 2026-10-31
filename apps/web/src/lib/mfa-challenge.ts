/**
 * The MFA challenge a password sign-in returned, kept for the code-entry page.
 * sessionStorage (this tab only, gone when it closes) rather than the URL, so
 * the token never lands in history or a shared link. It is short-lived and
 * only good for POST /auth/mfa/verify; it is not the password and not a
 * session.
 */
const KEY = "paca.mfa-challenge";

export interface PendingMfa {
	challenge: string;
	/** Same-origin path to open once signed in, if the user was sent here from one. */
	redirectTo?: string;
}

export function savePendingMfa(pending: PendingMfa): void {
	try {
		sessionStorage.setItem(KEY, JSON.stringify(pending));
	} catch {
		// Storage can be blocked; the user is then sent back to sign in.
	}
}

export function loadPendingMfa(): PendingMfa | null {
	try {
		const raw = sessionStorage.getItem(KEY);
		if (!raw) return null;
		const parsed = JSON.parse(raw) as Partial<PendingMfa>;
		return typeof parsed.challenge === "string" && parsed.challenge
			? { challenge: parsed.challenge, redirectTo: parsed.redirectTo }
			: null;
	} catch {
		return null;
	}
}

export function clearPendingMfa(): void {
	try {
		sessionStorage.removeItem(KEY);
	} catch {
		// Nothing to clear.
	}
}
