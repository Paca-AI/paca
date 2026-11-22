// Turning a participant's audio into utterances. Each remote participant has
// its own track, so the speaker is known without diarization; this only has to
// find where one turn of speech ends and the next begins.

export interface SegmenterOptions {
	sampleRate: number;
	/** RMS (0..32768) at or above which a frame counts as speech. */
	speechRms?: number;
	/** Silence that ends an utterance. */
	silenceMs?: number;
	/** Utterances shorter than this are noise and dropped. */
	minMs?: number;
	/** A long monologue is cut here so STT requests stay small. */
	maxMs?: number;
	/** Audio kept from before speech starts so the first word is not clipped. */
	preRollMs?: number;
}

export interface Utterance {
	pcm: Int16Array;
	sampleRate: number;
	startedAt: number;
	durationMs: number;
}

export function rms(frame: Int16Array): number {
	if (frame.length === 0) return 0;
	let sum = 0;
	for (let i = 0; i < frame.length; i++) {
		const s = frame[i] ?? 0;
		sum += s * s;
	}
	return Math.sqrt(sum / frame.length);
}

/** An energy-based utterance segmenter. Feed it frames; it returns an utterance
 * when one has just ended. flush() returns whatever is pending (call it when the
 * track ends). */
export class Segmenter {
	private readonly o: Required<SegmenterOptions>;
	private pre: Int16Array[] = [];
	private preSamples = 0;
	private cur: Int16Array[] = [];
	private curSamples = 0;
	private silentSamples = 0;
	private startedAt = 0;

	constructor(
		opts: SegmenterOptions,
		private readonly now: () => number = Date.now,
	) {
		this.o = {
			speechRms: 500,
			silenceMs: 700,
			minMs: 300,
			maxMs: 25_000,
			preRollMs: 200,
			...opts,
		};
	}

	private ms(samples: number): number {
		return (samples / this.o.sampleRate) * 1000;
	}

	push(frame: Int16Array): Utterance | null {
		const speech = rms(frame) >= this.o.speechRms;
		if (this.cur.length === 0) {
			if (!speech) {
				this.pre.push(frame);
				this.preSamples += frame.length;
				while (this.pre.length > 1 && this.ms(this.preSamples) > this.o.preRollMs) {
					this.preSamples -= this.pre.shift()?.length ?? 0;
				}
				return null;
			}
			this.startedAt = this.now() - this.ms(this.preSamples + frame.length);
			this.cur = this.pre;
			this.curSamples = this.preSamples;
			this.pre = [];
			this.preSamples = 0;
		}
		this.cur.push(frame);
		this.curSamples += frame.length;
		this.silentSamples = speech ? 0 : this.silentSamples + frame.length;
		if (this.ms(this.silentSamples) >= this.o.silenceMs || this.ms(this.curSamples) >= this.o.maxMs) {
			return this.take();
		}
		return null;
	}

	flush(): Utterance | null {
		return this.cur.length > 0 ? this.take() : null;
	}

	private take(): Utterance | null {
		const total = this.curSamples;
		const speechSamples = total - this.silentSamples;
		const pcm = new Int16Array(total);
		let at = 0;
		for (const f of this.cur) {
			pcm.set(f, at);
			at += f.length;
		}
		const out = { pcm, sampleRate: this.o.sampleRate, startedAt: this.startedAt, durationMs: this.ms(total) };
		this.cur = [];
		this.curSamples = 0;
		this.silentSamples = 0;
		return this.ms(speechSamples) >= this.o.minMs ? out : null;
	}
}

/** Wraps 16-bit mono PCM in a WAV container. */
export function toWav(pcm: Int16Array, sampleRate: number): Buffer {
	const data = Buffer.from(pcm.buffer, pcm.byteOffset, pcm.byteLength);
	const header = Buffer.alloc(44);
	header.write("RIFF", 0);
	header.writeUInt32LE(36 + data.length, 4);
	header.write("WAVE", 8);
	header.write("fmt ", 12);
	header.writeUInt32LE(16, 16);
	header.writeUInt16LE(1, 20); // PCM
	header.writeUInt16LE(1, 22); // mono
	header.writeUInt32LE(sampleRate, 24);
	header.writeUInt32LE(sampleRate * 2, 28);
	header.writeUInt16LE(2, 32);
	header.writeUInt16LE(16, 34);
	header.write("data", 36);
	header.writeUInt32LE(data.length, 40);
	return Buffer.concat([header, data]);
}
