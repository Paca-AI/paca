import { createHmac } from "node:crypto";
import {
	type APIRequest,
	type APIRequestContext,
	type APIResponse,
	expect,
} from "@playwright/test";
import { API_URL, authRequest, RESTRICTED_PASSWORD } from "./e2e-api";

export type PlaywrightLike = { request: Pick<APIRequest, "newContext"> };

const B32 = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
const PERIOD_SECONDS = 30;

function base32Decode(input: string): Buffer {
	let bits = 0;
	let value = 0;
	const out: number[] = [];
	for (const ch of input.replace(/=+$/, "").toUpperCase()) {
		value = (value << 5) | B32.indexOf(ch);
		bits += 5;
		if (bits >= 8) {
			out.push((value >>> (bits - 8)) & 0xff);
			bits -= 8;
		}
	}
	return Buffer.from(out);
}

/** RFC 6238 code (SHA-1, 6 digits, 30 s) for an explicit time step. */
export function totpForStep(secret: string, step: number): string {
	const msg = Buffer.alloc(8);
	msg.writeBigUInt64BE(BigInt(step));
	const mac = createHmac("sha1", base32Decode(secret)).update(msg).digest();
	const off = mac[mac.length - 1] & 0x0f;
	const bin = mac.readUInt32BE(off) & 0x7fffffff;
	return String(bin % 1_000_000).padStart(6, "0");
}

const currentStep = () => Math.floor(Date.now() / 1000 / PERIOD_SECONDS);

// The server accepts a step only if it is newer than the last one it accepted
// (replay protection) and within one step of its clock. Remember what each
// secret has used so successive sign-ins in one test don't collide.
const lastStep = new Map<string, number>();

/**
 * A code the server will accept now and has not seen: strictly newer than the
 * last one handed out for `secret`. Waits for the clock when the three-step
 * window (previous, current, next) has no unused step left.
 */
export async function freshCode(secret: string): Promise<string> {
	for (;;) {
		const now = currentStep();
		const next = Math.max((lastStep.get(secret) ?? 0) + 1, now - 1);
		if (next <= now + 1) {
			lastStep.set(secret, next);
			return totpForStep(secret, next);
		}
		await new Promise((r) => setTimeout(r, 1_000));
	}
}

/** Marks a step as already consumed outside freshCode (e.g. by the UI). */
export function noteUsed(secret: string): void {
	lastStep.set(secret, Math.max(lastStep.get(secret) ?? 0, currentStep()));
}

export async function errorCode(response: APIResponse): Promise<string> {
	const body = await response.json();
	return (body?.error_code ?? "") as string;
}

export interface Enrolled {
	secret: string;
	recoveryCodes: string[];
}

/** Turns MFA on for the signed-in user of `ctx`; returns the secret and codes. */
export async function enrollMfa(ctx: APIRequestContext): Promise<Enrolled> {
	const begin = await ctx.post(`${API_URL}/users/me/mfa/enroll`);
	expect(begin.ok()).toBeTruthy();
	const secret = (await begin.json()).data.secret as string;
	const confirm = await ctx.post(`${API_URL}/users/me/mfa/confirm`, {
		data: { code: await freshCode(secret) },
	});
	expect(confirm.ok()).toBeTruthy();
	const recoveryCodes = (await confirm.json()).data.recovery_codes as string[];
	return { secret, recoveryCodes };
}

/** POST /auth/login with no session carried over. */
export async function rawLogin(
	playwright: PlaywrightLike,
	username: string,
	password = RESTRICTED_PASSWORD,
): Promise<{ ctx: APIRequestContext; response: APIResponse }> {
	const ctx = await playwright.request.newContext();
	const response = await ctx.post(`${API_URL}/auth/login`, {
		data: { username, password },
	});
	return { ctx, response };
}

/** Step 2 of an MFA sign-in. */
export function verifyMfa(
	ctx: APIRequestContext,
	challenge: string,
	code: string,
): Promise<APIResponse> {
	return ctx.post(`${API_URL}/auth/mfa/verify`, { data: { challenge, code } });
}

/** Signs in fully (password, then code) and returns the authenticated context. */
export async function signInWithMfa(
	playwright: PlaywrightLike,
	username: string,
	code: string,
): Promise<APIRequestContext> {
	const { ctx, response } = await rawLogin(playwright, username);
	expect(response.ok()).toBeTruthy();
	const body = (await response.json()).data;
	expect(body.mfa_required).toBe(true);
	const verified = await verifyMfa(ctx, body.mfa_challenge, code);
	expect(verified.ok()).toBeTruthy();
	return ctx;
}

export interface Policy {
	privileged_roles: string[];
	require_mfa: boolean;
	ip_allow_list: string[];
}

export const DEFAULT_POLICY: Policy = {
	privileged_roles: ["*"],
	require_mfa: false,
	ip_allow_list: [],
};

/** Admin-only: replaces the instance login policy. */
export async function putPolicy(
	admin: APIRequestContext,
	policy: Policy,
): Promise<APIResponse> {
	return admin.put(`${API_URL}/admin/auth-security`, { data: policy });
}

/** Puts the instance policy back to its install default (nothing enforced). */
export async function restorePolicy(admin: APIRequestContext): Promise<void> {
	await authRequest(admin);
	await putPolicy(admin, DEFAULT_POLICY);
}

export async function setUserCidrs(
	admin: APIRequestContext,
	userId: string,
	cidrs: string[],
): Promise<APIResponse> {
	return admin.put(`${API_URL}/admin/users/${userId}/allowed-cidrs`, {
		data: { allowed_cidrs: cidrs },
	});
}
