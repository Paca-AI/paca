import { describe, expect, it } from "vitest";
import { repoNameFromRemote } from "../../cli/git-source.js";

describe("repoNameFromRemote", () => {
	it.each([
		["git@github.com:Acme/widgets.git", "Acme/widgets"],
		["https://github.com/Acme/widgets", "Acme/widgets"],
		["https://user:secret@github.com/Acme/widgets.git/", "Acme/widgets"],
		["ssh://git@host:22/group/sub/widgets.git", "sub/widgets"],
		["/srv/git/widgets.git", "git/widgets"],
		["https://host", null],
	])("%s -> %s", (url, want) => {
		expect(repoNameFromRemote(url)).toBe(want);
	});
});
