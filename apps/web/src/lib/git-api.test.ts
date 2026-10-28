import { afterEach, describe, expect, it, vi } from "vitest";
import {
	followGitStep,
	forgetGitFlow,
	GIT_FALLBACK_PATH,
	gitErrorKey,
	gitProviderMessage,
	readGitFlow,
	rememberGitFlow,
	repoOwnerOf,
	returnPathOf,
} from "./git-api";

const ORIGIN = "https://paca.example.com";

describe("returnPathOf", () => {
	it("turns a same-origin return URL into a path", () => {
		expect(returnPathOf(`${ORIGIN}/admin/git?x=1#h`, ORIGIN)).toBe(
			"/admin/git?x=1#h",
		);
	});

	it("never leaves the instance, whatever the API says", () => {
		for (const bad of [
			"https://evil.example/steal",
			"//evil.example/x",
			"javascript:alert(1)",
			"http://paca.example.com/x", // other scheme = other origin
			"",
			null,
			undefined,
		]) {
			expect(returnPathOf(bad, ORIGIN)).toBe(GIT_FALLBACK_PATH);
		}
	});
});

describe("gitErrorKey", () => {
	const apiError = (code: string) => ({
		response: { data: { error_code: code } },
	});

	it("maps known Git error codes", () => {
		expect(gitErrorKey(apiError("GIT_SETUP_INVALID"))).toBe("setupInvalid");
		expect(gitErrorKey(apiError("GIT_IDENTITY_EXISTS"))).toBe("identityExists");
		expect(gitErrorKey(apiError("GIT_PUBLIC_URL_REQUIRED"))).toBe(
			"publicUrlRequired",
		);
		expect(gitErrorKey(apiError("GIT_PROVIDER_UNAVAILABLE"))).toBe(
			"providerUnavailable",
		);
	});

	it("falls back to a generic message", () => {
		expect(gitErrorKey(apiError("SOMETHING_ELSE"))).toBe("generic");
		expect(gitErrorKey(new Error("boom"))).toBe("generic");
	});
});

describe("followGitStep", () => {
	afterEach(() => {
		document.body.innerHTML = "";
		vi.restoreAllMocks();
	});

	it("posts the manifest as a hidden form field", () => {
		const submit = vi
			.spyOn(HTMLFormElement.prototype, "submit")
			.mockImplementation(() => {});
		const followed = followGitStep({
			kind: "form_post",
			url: "https://github.com/settings/apps/new?state=s",
			fields: { manifest: '{"name":"Paca"}' },
		});
		expect(followed).toBe(true);
		expect(submit).toHaveBeenCalledOnce();
		const form = document.querySelector("form");
		expect(form?.method).toBe("post");
		expect(form?.action).toBe("https://github.com/settings/apps/new?state=s");
		const input = form?.querySelector<HTMLInputElement>("input[name=manifest]");
		expect(input?.type).toBe("hidden");
		expect(input?.value).toBe('{"name":"Paca"}');
	});

	it("does nothing for steps that need no navigation", () => {
		const submit = vi
			.spyOn(HTMLFormElement.prototype, "submit")
			.mockImplementation(() => {});
		expect(followGitStep({ kind: "done" })).toBe(false);
		expect(followGitStep({ kind: "token_form" })).toBe(false);
		expect(followGitStep({ kind: "redirect" })).toBe(false); // no URL
		expect(submit).not.toHaveBeenCalled();
	});
});

describe("repoOwnerOf", () => {
	it("finds the owner in every common spelling of a repository", () => {
		for (const [input, owner] of [
			["acme/web", "acme"],
			["  acme/web  ", "acme"],
			["https://github.com/acme/web", "acme"],
			["https://github.com/acme/web.git", "acme"],
			["https://github.com/acme/web/pull/3", "acme"],
			["https://gitlab.example.com/group/sub/project", "group"],
			["git@github.com:acme/web.git", "acme"],
			["github.com/acme/web", "acme"],
		] as const) {
			expect(repoOwnerOf(input), input).toBe(owner);
		}
	});

	it("returns nothing it cannot be sure of", () => {
		for (const input of [
			"",
			"   ",
			"web",
			"https://github.com",
			"https://",
			"::",
		]) {
			expect(repoOwnerOf(input), input).toBe("");
		}
	});
});

describe("git flow hints", () => {
	afterEach(() => window.sessionStorage.clear());

	it("remembers an access grant by its state", () => {
		rememberGitFlow("st1", { kind: "access", projectId: "p1" });
		expect(readGitFlow("st1")).toEqual({ kind: "access", projectId: "p1" });
		forgetGitFlow("st1");
		expect(readGitFlow("st1")).toEqual({ kind: "identity" });
	});

	it("treats an unknown state as an identity setup", () => {
		expect(readGitFlow("never-seen")).toEqual({ kind: "identity" });
		rememberGitFlow(undefined, { kind: "access", projectId: "p" }); // no state: nothing stored
		expect(window.sessionStorage.length).toBe(0);
	});

	it("ignores a damaged or forged hint", () => {
		for (const raw of [
			"not json",
			'{"kind":"access"}',
			'{"kind":"access","projectId":5}',
			'{"kind":"other"}',
		]) {
			window.sessionStorage.setItem("paca.git.flow.bad", raw);
			expect(readGitFlow("bad")).toEqual({ kind: "identity" });
		}
	});

	it("survives unavailable storage", () => {
		const spy = vi
			.spyOn(Storage.prototype, "getItem")
			.mockImplementation(() => {
				throw new Error("blocked");
			});
		expect(readGitFlow("x")).toEqual({ kind: "identity" });
		spy.mockRestore();
	});
});

describe("gitProviderMessage", () => {
	const withMsg = (m: unknown) => ({
		response: { data: { error_details: { provider_message: m } } },
	});
	it("returns the trimmed message, capped", () => {
		expect(gitProviderMessage(withMsg("  covers only acme/ui "))).toBe(
			"covers only acme/ui",
		);
		expect(gitProviderMessage(withMsg("x".repeat(900)))?.length).toBe(501);
	});
	it("is null when there is none", () => {
		expect(gitProviderMessage(withMsg("  "))).toBeNull();
		expect(gitProviderMessage(withMsg(42))).toBeNull();
		expect(
			gitProviderMessage({
				response: { data: { error_code: "GIT_PROVIDER_REJECTED" } },
			}),
		).toBeNull();
		expect(gitProviderMessage(new Error("boom"))).toBeNull();
	});
});
