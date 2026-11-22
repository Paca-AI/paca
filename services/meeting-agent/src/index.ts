import { fileURLToPath } from "node:url";
import { cli, defineAgent, type JobContext, ServerOptions } from "@livekit/agents";
import { loadConfig } from "./config.ts";
import { runJob } from "./job.ts";

export default defineAgent({
	entry: async (ctx: JobContext) => {
		await runJob(ctx, loadConfig());
	},
});

// `node src/index.ts start|dev` runs the worker; importing this file (a job
// process loads it as the agent module) does not.
if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
	const cfg = loadConfig();
	// The CLI reads these itself (--url and friends default to them) and lets
	// them override ServerOptions, so hand it the normalised values.
	process.env.LIVEKIT_URL = cfg.livekitUrl;
	process.env.LIVEKIT_API_KEY = cfg.livekitApiKey;
	process.env.LIVEKIT_API_SECRET = cfg.livekitApiSecret;
	cli.runApp(
		new ServerOptions({
			agent: fileURLToPath(import.meta.url),
			agentName: cfg.agentName,
			wsURL: cfg.livekitUrl,
			apiKey: cfg.livekitApiKey,
			apiSecret: cfg.livekitApiSecret,
		}),
	);
}
