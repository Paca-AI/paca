// spec: features/projects/chat-assistant.feature
// seed: tests/seed.spec.ts
//
// Phase 3b of chat: the meeting assistant. Needs the e2e stack with LiveKit
// reachable from the browser and the meeting-agent worker running against it
// (docker compose --profile meeting, or `npm start` in services/meeting-agent
// with the e2e LIVEKIT_* values). The agent's STT and model endpoints are faked
// by this spec; they must be reachable from the worker at E2E_ASSISTANT_MODEL_URL
// (default http://localhost:18081, which is right for a worker on this machine).
// Skipped when assistants are off, and outside Chromium (fake media).

import http from "node:http";
import { expect, test } from "@playwright/test";
import {
	API_URL,
	authRequest,
	BASE_URL,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
	createUserWithProjectPermissions,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import { setRole } from "../helpers/role-select";

const TEST_PREFIX = "E2E_ASSISTANT_";
const RUN_ID = newRunId();
const MODEL_URL =
	process.env.E2E_ASSISTANT_MODEL_URL ?? "http://localhost:18081";
const MODEL_PORT = Number(new URL(MODEL_URL).port || 18081);
const SAID = "we should fix the login page on safari";

test.use({
	permissions: ["microphone", "camera"],
	launchOptions: {
		args: [
			"--use-fake-device-for-media-stream",
			"--use-fake-ui-for-media-stream",
		],
	},
});

/** Answers the worker's two outbound calls: transcription and the model. */
function startFakeModels(): Promise<http.Server> {
	const server = http.createServer((req, res) => {
		let body = "";
		req.on("data", (c) => {
			body += c;
		});
		req.on("end", () => {
			const json = (o: unknown) => {
				res.writeHead(200, { "Content-Type": "application/json" });
				res.end(JSON.stringify(o));
			};
			if (req.url?.endsWith("/audio/transcriptions")) {
				return json({ text: SAID });
			}
			if (req.url?.endsWith("/chat/completions")) {
				if (JSON.parse(body).tools) {
					return json({
						choices: [
							{
								message: {
									content: null,
									tool_calls: [
										{
											function: {
												name: "propose_task",
												arguments: JSON.stringify({
													title: "Fix the login page",
													description: "Breaks on Safari",
													priority: "high",
												}),
											},
										},
									],
								},
							},
						],
					});
				}
				return json({
					choices: [{ message: { content: "We discussed the login bug." } }],
				});
			}
			res.writeHead(404);
			res.end();
		});
	});
	return new Promise((resolve) =>
		server.listen(MODEL_PORT, () => resolve(server)),
	);
}

test.describe("Meeting assistant", () => {
	let projectId: string;
	let channelId: string;
	let agentName: string;
	let models: http.Server;

	test.beforeEach(async ({ request, page }, info) => {
		test.skip(info.project.name !== "chromium", "fake media is Chromium-only");
		await authRequest(request);
		const cfg = await request.get(`${API_URL}/chat/meeting/config`);
		test.skip(
			!cfg.ok() || (await cfg.json()).data?.enabled !== true,
			"meeting assistants are not enabled on this stack",
		);
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
		await cleanupUsersByPrefix(request, TEST_PREFIX);
		models = await startFakeModels();
		projectId = await createProject(request, `${TEST_PREFIX}PROJECT_${RUN_ID}`);

		const channel = await request.post(
			`${API_URL}/projects/${projectId}/channels`,
			{ data: { kind: "voice", name: `lounge${RUN_ID}`.toLowerCase() } },
		);
		expect(channel.ok()).toBeTruthy();
		channelId = (await channel.json()).data.id as string;

		const roles = await request.get(
			`${API_URL}/projects/${projectId}/roles?page_size=100`,
		);
		const roleList = (await roles.json()).data;
		const roleId = (roleList.items ?? roleList).find(
			(r: { name: string }) => r.name === "Meeting assistant",
		).id as string;
		agentName = `Atlas ${RUN_ID}`;
		const agent = await request.post(
			`${API_URL}/projects/${projectId}/agents`,
			{
				data: {
					name: agentName,
					handle: `atlas${RUN_ID}`.toLowerCase(),
					agent_type: "meeting",
					llm_provider: "openai",
					llm_model: "fake",
					llm_api_key: "fake",
					llm_base_url: MODEL_URL,
					role_ids: [roleId],
					// How it transcribes is part of creating it.
					meeting_settings: {
						stt_provider: "openai",
						stt_model: "fake",
						stt_base_url: MODEL_URL,
						// Suggestions and notes are options that use the model; these
						// tests turn them on.
						mode: "on_demand",
						summary_target: "channel_message",
					},
				},
			},
		);
		expect(agent.ok()).toBeTruthy();
		const agentId = (await agent.json()).data.id as string;
		const settings = await request.patch(
			`${API_URL}/projects/${projectId}/agents/${agentId}/meeting-settings`,
			{
				data: {
					store_transcript: true,
					transcript_retention_days: 7,
				},
			},
		);
		expect(settings.ok()).toBeTruthy();
		await signIn(page);
	});

	test.afterEach(async ({ request }) => {
		models?.close();
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
		await cleanupUsersByPrefix(request, TEST_PREFIX);
	});

	const chatUrl = () => `${BASE_URL}/projects/${projectId}/chat/${channelId}`;
	const assistants = async (
		request: import("@playwright/test").APIRequestContext,
	) =>
		(
			await (
				await request.get(
					`${API_URL}/projects/${projectId}/channels/${channelId}/call/assistants`,
				)
			).json()
		).data as { status: string }[];

	test("the assistant listens, suggests a task and the person creates it", async ({
		page,
		request,
	}) => {
		test.setTimeout(180_000);
		await page.goto(chatUrl());
		const [call] = await Promise.all([
			page.waitForEvent("popup"),
			page.getByRole("button", { name: "Join call" }).first().click(),
		]);
		await expect(call.getByRole("region", { name: "Call" })).toBeVisible();

		// The picker says what each assistant does and keeps before it is added.
		await call.getByRole("button", { name: "Add assistant" }).click();
		await expect(
			call.getByText(
				/Transcribes, suggests when asked, writes notes · transcript kept 7 days/,
			),
		).toBeVisible();
		await call.getByRole("button", { name: new RegExp(agentName) }).click();

		// The consent indicator is mandatory.
		await expect(
			call.getByText(`${agentName} is listening and transcribing`),
		).toBeVisible({ timeout: 60_000 });
		await expect(call.getByText("Transcript kept for 7 days.")).toBeVisible();

		// The fake microphone beeps; the worker hears it and the fake STT answers.
		await expect(
			call.getByRole("log", { name: "Live captions" }).getByText(SAID).first(),
		).toBeVisible({ timeout: 60_000 });

		// The whole transcript is a click away while the call goes on.
		await call.getByRole("button", { name: "Transcript", exact: true }).click();
		await expect(
			call.getByRole("list", { name: "Transcript" }).getByText(SAID).first(),
		).toBeVisible({ timeout: 30_000 });

		await call.getByRole("button", { name: "Suggest tasks" }).click();
		const title = call.getByRole("textbox", { name: "Title" });
		await expect(title).toHaveValue("Fix the login page", { timeout: 60_000 });

		await title.fill("Fix the login page now");
		await call.getByRole("button", { name: "Create task" }).click();
		await expect(title).toHaveCount(0, { timeout: 30_000 });

		const tasks = await request.get(`${API_URL}/projects/${projectId}/tasks`);
		expect(JSON.stringify(await tasks.json())).toContain(
			"Fix the login page now",
		);

		await call.getByRole("button", { name: "Leave call" }).click();
		await expect
			.poll(async () => (await assistants(request)).length, { timeout: 60_000 })
			.toBe(0);

		// Afterwards the transcript is in the channel's meeting history, to read
		// and to save.
		await page.goto(chatUrl());
		await page.getByRole("button", { name: "Meeting history" }).click();
		const history = page.getByRole("dialog", { name: "Meeting history" });
		await expect(history.getByText(SAID).first()).toBeVisible({
			timeout: 30_000,
		});
		const [file] = await Promise.all([
			page.waitForEvent("download"),
			history.getByRole("button", { name: "Download transcript" }).click(),
		]);
		expect(file.suggestedFilename()).toMatch(/^transcript-.*\.txt$/);
	});

	test("someone who cannot invite agents is offered no way to add one", async ({
		request,
		playwright,
		browser,
	}) => {
		test.setTimeout(90_000);
		const member = `${TEST_PREFIX}member_${RUN_ID}`;
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username: member,
			roleName: `${TEST_PREFIX}role_${RUN_ID}`,
			permissions: {
				"channels:read": true,
				"messages:write": true,
				"calls:join": true,
			},
		});
		const ctx = await browser.newContext({
			permissions: ["microphone", "camera"],
		});
		const page = await ctx.newPage();
		await signIn(page, member, RESTRICTED_PASSWORD);
		await page.goto(chatUrl());
		const [call] = await Promise.all([
			page.waitForEvent("popup"),
			page.getByRole("button", { name: "Join call" }).first().click(),
		]);
		await expect(call.getByRole("region", { name: "Call" })).toBeVisible();
		await expect(
			call.getByRole("button", { name: "Add assistant" }),
		).toHaveCount(0);
		// Recording needs calls:record too.
		await expect(
			call.getByRole("button", { name: "Start recording" }),
		).toHaveCount(0);
		await ctx.close();
	});

	test("a meeting assistant is created from the Agents page and configured on its Assistant tab", async ({
		page,
		request,
	}) => {
		test.setTimeout(120_000);
		const name = `Scribe ${RUN_ID}`;
		await page.goto(`${BASE_URL}/projects/${projectId}/agents`);
		await page.getByRole("button", { name: "New Agent" }).click();
		const dialog = page.getByRole("dialog", { name: "Create AI Agent" });
		await expect(dialog).toBeVisible();

		await dialog.getByRole("button", { name: /Meeting assistant/ }).click();
		await dialog.getByRole("textbox", { name: /^Name/ }).fill(name);
		await dialog.getByRole("button", { name: "Continue" }).click();
		await expect(dialog.getByText("2 / 3")).toBeVisible();

		// A meeting assistant has its reasoning model but no environment, Docker
		// or git identity to configure.
		await expect(dialog.getByText("Docker")).toHaveCount(0);
		// Transcription is the job: no model, key or instructions are asked for
		// unless notes or suggestions are turned on.
		await expect(dialog.getByLabel(/^API Key/)).toHaveCount(0);
		// It cannot be created without how it transcribes.
		const next = dialog.getByRole("button", { name: "Continue" });
		await expect(next).toBeDisabled();
		await dialog
			.getByRole("combobox", { name: "Transcription provider" })
			.click();
		await page.getByRole("option", { name: "openai" }).click();
		await dialog.getByLabel(/^Transcription model/).fill("whisper-1");
		await expect(next).toBeDisabled();
		await dialog.getByLabel("Transcription API key").fill("sk-stt-e2e");
		await expect(next).toBeEnabled();

		// Turning notes on brings in the assistant's model, which is then needed.
		await dialog.getByRole("switch", { name: /Write meeting notes/ }).click();
		await expect(next).toBeDisabled();
		await dialog.getByLabel(/^API Key/).fill("sk-e2e-placeholder-key");
		const baseUrl = dialog.getByRole("textbox", { name: /^Base URL/ });
		if ((await baseUrl.inputValue()) === "") {
			await baseUrl.fill("https://api.anthropic.com");
		}
		await expect(next).toBeEnabled();
		await next.click();
		await expect(dialog.getByText("3 / 3")).toBeVisible();
		await setRole(page, dialog, "Project Role", "Meeting assistant", true);
		await dialog.getByRole("button", { name: "Create Agent" }).click();
		await expect(dialog).toBeHidden({ timeout: 30_000 });

		const listed = await request.get(`${API_URL}/projects/${projectId}/agents`);
		const agents = (await listed.json()).data.items as {
			id: string;
			name: string;
			agent_type: string;
		}[];
		const created = agents.find((a) => a.name === name);
		expect(created?.agent_type).toBe("meeting");

		// Its page has an Assistant tab and none of the tabs that need a runner.
		await page.goto(`${BASE_URL}/projects/${projectId}/agents/${created?.id}`);
		await expect(page.getByRole("tab", { name: "Assistant" })).toBeVisible();
		await expect(page.getByRole("tab", { name: "Git" })).toHaveCount(0);
		await expect(page.getByRole("tab", { name: "MCP Servers" })).toHaveCount(0);
		await page.getByRole("tab", { name: "Assistant" }).click();

		// What was entered at creation is there, and the key is never shown.
		await expect(page.getByLabel("Transcription model")).toHaveValue(
			"whisper-1",
		);
		await expect(page.getByLabel("Transcription API key")).toHaveValue("");
		await page.getByLabel("Transcription model").fill("whisper-large");
		await page.getByLabel("Keep it for (days)").fill("14");
		await page.getByRole("button", { name: "Save changes" }).click();

		await expect
			.poll(async () => {
				const r = await request.get(
					`${API_URL}/projects/${projectId}/agents/${created?.id}/meeting-settings`,
				);
				const d = (await r.json()).data;
				return `${d.stt_provider}|${d.stt_model}|${d.has_stt_key}|${d.transcript_retention_days}`;
			})
			.toBe("openai|whisper-large|true|14");
		// The key never comes back to the page.
		await expect(page.getByLabel("Transcription API key")).toHaveValue("");
	});

	test("a call is recorded, everyone is told, and the recording plays afterwards", async ({
		page,
		request,
	}) => {
		test.setTimeout(240_000);
		const cfg = await request.get(`${API_URL}/chat/calls/config`);
		test.skip(
			(await cfg.json()).data?.recording !== true,
			"call recording is not enabled on this stack",
		);
		await page.goto(chatUrl());
		const [call] = await Promise.all([
			page.waitForEvent("popup"),
			page.getByRole("button", { name: "Join call" }).first().click(),
		]);
		await expect(call.getByRole("region", { name: "Call" })).toBeVisible();

		await call.getByRole("button", { name: "Start recording" }).click();
		// Starting asks first: everyone in the call is told.
		await expect(call.getByText("Record this call?")).toBeVisible();
		await call.getByRole("button", { name: "Start recording" }).last().click();
		await expect(call.getByText("This call is being recorded.")).toBeVisible({
			timeout: 60_000,
		});
		await expect(
			call.getByRole("button", { name: "Stop recording" }),
		).toBeVisible();

		await call.waitForTimeout(8_000);
		await call.getByRole("button", { name: "Stop recording" }).click();
		await expect(call.getByText("This call is being recorded.")).toBeHidden({
			timeout: 60_000,
		});
		await call.getByRole("button", { name: "Leave call" }).click();

		// The file is finalised by LiveKit's egress, then listed with the call.
		await expect
			.poll(
				async () => {
					const r = await request.get(
						`${API_URL}/projects/${projectId}/channels/${channelId}/recordings`,
					);
					const list = (await r.json()).data as { status: string }[];
					return list[0]?.status;
				},
				{ timeout: 120_000 },
			)
			.toBe("complete");

		await page.goto(chatUrl());
		await page.getByRole("button", { name: "Meeting history" }).click();
		const history = page.getByRole("dialog", { name: "Meeting history" });
		await history.getByRole("button", { name: /Play recording/ }).click();
		const video = history.locator("video");
		await expect(video).toBeVisible();
		await expect
			.poll(async () => video.evaluate((v: HTMLVideoElement) => v.readyState), {
				timeout: 30_000,
			})
			.toBeGreaterThan(0);
	});

	test("an assistant with no model transcribes, and offers no suggestions", async ({
		page,
		request,
	}) => {
		test.setTimeout(150_000);
		// Transcription alone: no model, key or instructions at all.
		const roles = await request.get(
			`${API_URL}/projects/${projectId}/roles?page_size=100`,
		);
		const list = (await roles.json()).data;
		const roleId = (list.items ?? list).find(
			(r: { name: string }) => r.name === "Meeting assistant",
		).id as string;
		const name = `Plain ${RUN_ID}`;
		const created = await request.post(
			`${API_URL}/projects/${projectId}/agents`,
			{
				data: {
					name,
					handle: `plain${RUN_ID}`.toLowerCase(),
					agent_type: "meeting",
					role_ids: [roleId],
					meeting_settings: {
						stt_provider: "openai",
						stt_model: "fake",
						stt_base_url: MODEL_URL,
					},
				},
			},
		);
		expect(created.ok()).toBeTruthy();

		await page.goto(chatUrl());
		const [call] = await Promise.all([
			page.waitForEvent("popup"),
			page.getByRole("button", { name: "Join call" }).first().click(),
		]);
		await expect(call.getByRole("region", { name: "Call" })).toBeVisible();
		await call.getByRole("button", { name: "Add assistant" }).click();
		await expect(
			call.getByText(/Transcribes the call · transcript kept 30 days/),
		).toBeVisible();
		await call.getByRole("button", { name: new RegExp(name) }).click();
		await expect(
			call.getByText(`${name} is listening and transcribing`),
		).toBeVisible({ timeout: 60_000 });
		await expect(
			call.getByRole("log", { name: "Live captions" }).getByText(SAID).first(),
		).toBeVisible({ timeout: 60_000 });
		// Suggestions are off, so there is nothing to ask for.
		await expect(
			call.getByRole("button", { name: "Suggest tasks" }),
		).toHaveCount(0);
	});
});
