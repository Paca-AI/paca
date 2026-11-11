// Backstop for a missed "iam.changed" announcement.
//
// The API bumps a counter (paca:iam:epoch) before it announces an IAM change on
// Pub/Sub. Pub/Sub keeps nothing, so a replica that was reconnecting when the
// announcement went out would keep deciding on stale rooms and cached answers
// until its next periodic pass. Each replica polls the counter instead: a value
// above the last one it saw means an announcement went by unseen, and it reacts
// as it would to the announcement.
//
// The announcement carries the epoch it produced, so one that arrives is
// recorded and the poll does not react to the same change a second time.

export interface IamEpochOptions {
	read: () => Promise<number | null>;
	onChange: () => void;
	intervalMs?: number;
}

export const DEFAULT_IAM_EPOCH_INTERVAL_MS = 5_000;

export class IamEpochWatcher {
	private last: number | null = null;
	private timer: ReturnType<typeof setInterval> | undefined;
	private polling = false;

	constructor(private readonly opts: IamEpochOptions) {}

	// observe records the epoch of an announcement that was received.
	observe(epoch: number | undefined): void {
		if (typeof epoch !== "number" || !Number.isFinite(epoch)) return;
		if (this.last === null || epoch > this.last) this.last = epoch;
	}

	// poll reads the counter once. The first read only establishes the
	// baseline: changes before this replica started are not its news.
	async poll(): Promise<void> {
		if (this.polling) return;
		this.polling = true;
		try {
			const epoch = await this.opts.read();
			if (epoch === null) return;
			if (this.last === null) {
				this.last = epoch;
				return;
			}
			if (epoch > this.last) {
				this.last = epoch;
				this.opts.onChange();
			}
		} catch {
			// Valkey unreachable: try again on the next tick.
		} finally {
			this.polling = false;
		}
	}

	start(): void {
		const every = this.opts.intervalMs ?? DEFAULT_IAM_EPOCH_INTERVAL_MS;
		this.timer = setInterval(() => void this.poll(), every);
		this.timer.unref?.();
		void this.poll();
	}

	stop(): void {
		if (this.timer) clearInterval(this.timer);
		this.timer = undefined;
	}
}
