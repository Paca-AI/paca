import { describe, expect, it } from "vitest";
import { matchAnnotationLink, matchAnnotationLinkOnly } from "./annotation-link";

const url =
	"https://paca.example.com/projects/p1/environments/e1/port-forwards/pf1/comments/a1";

describe("matchAnnotationLink", () => {
	it("finds a link anywhere inside a longer string", () => {
		const match = matchAnnotationLink(`see this: ${url} — thanks`);
		expect(match).toEqual({
			projectId: "p1",
			environmentId: "e1",
			portForwardId: "pf1",
			annotationId: "a1",
		});
	});

	it("returns null when no link is present", () => {
		expect(matchAnnotationLink("just some text")).toBeNull();
	});
});

describe("matchAnnotationLinkOnly", () => {
	it("matches when the text is exactly the link", () => {
		expect(matchAnnotationLinkOnly(url)).toEqual({
			projectId: "p1",
			environmentId: "e1",
			portForwardId: "pf1",
			annotationId: "a1",
		});
	});

	it("matches when the link has surrounding whitespace", () => {
		expect(matchAnnotationLinkOnly(`  ${url}  \n`)).toEqual({
			projectId: "p1",
			environmentId: "e1",
			portForwardId: "pf1",
			annotationId: "a1",
		});
	});

	it("does not match when other text shares the paragraph/paste", () => {
		expect(matchAnnotationLinkOnly(`See this bug: ${url} — fix before Friday.`)).toBeNull();
	});

	it("does not match when there is no link at all", () => {
		expect(matchAnnotationLinkOnly("just some text")).toBeNull();
	});
});
