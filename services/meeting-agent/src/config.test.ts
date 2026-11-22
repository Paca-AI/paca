import assert from "node:assert/strict";
import { test } from "node:test";
import { loadConfig, withTrailingSlash } from "./config.ts";

const base = {
	LIVEKIT_URL: "ws://localhost:3000/livekit",
	LIVEKIT_API_KEY: "k",
	LIVEKIT_API_SECRET: "s",
	PACA_API_URL: "http://api:8080/api/v1/",
	MEETING_AGENT_TOKEN: "t",
};

test("the signalling URL gets the trailing slash the agents library needs", () => {
	assert.equal(withTrailingSlash("ws://livekit:7880"), "ws://livekit:7880/");
	assert.equal(withTrailingSlash("ws://h/livekit/"), "ws://h/livekit/");
	assert.equal(loadConfig(base).livekitUrl, "ws://localhost:3000/livekit/");
});

test("the internal URL wins and the API URL loses its trailing slash", () => {
	const c = loadConfig({ ...base, LIVEKIT_INTERNAL_WS_URL: "ws://livekit:7880" });
	assert.equal(c.livekitUrl, "ws://livekit:7880/");
	assert.equal(c.pacaApiUrl, "http://api:8080/api/v1");
	assert.equal(c.agentName, "paca-meeting");
});

test("a missing variable is named", () => {
	assert.throws(() => loadConfig({ ...base, MEETING_AGENT_TOKEN: "" }), /MEETING_AGENT_TOKEN/);
});
