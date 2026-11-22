import assert from "node:assert/strict";
import { test } from "node:test";
import { rms, Segmenter, toWav } from "./audio.ts";

const SR = 16_000;
const frame = (amp: number, ms = 20) => new Int16Array((SR * ms) / 1000).fill(amp);

function run(seg: Segmenter, parts: [number, number][]) {
	const out = [];
	for (const [amp, ms] of parts) {
		for (let i = 0; i < ms / 20; i++) {
			const u = seg.push(frame(amp));
			if (u) out.push(u);
		}
	}
	return out;
}

test("rms of silence and a constant signal", () => {
	assert.equal(rms(new Int16Array(0)), 0);
	assert.equal(rms(frame(0)), 0);
	assert.equal(Math.round(rms(frame(1000))), 1000);
});

test("one utterance ends after the silence gap", () => {
	const seg = new Segmenter({ sampleRate: SR });
	const out = run(seg, [
		[0, 400],
		[3000, 1000],
		[0, 1000],
	]);
	assert.equal(out.length, 1);
	const u = out[0];
	assert.ok(u && u.durationMs >= 1000 && u.durationMs < 2200, `duration ${u?.durationMs}`);
	// pre-roll keeps the first word: the utterance starts earlier than the first speech frame
	assert.ok((u?.pcm.length ?? 0) > SR, "contains the speech");
});

test("two turns are two utterances", () => {
	const seg = new Segmenter({ sampleRate: SR });
	const out = run(seg, [
		[3000, 600],
		[0, 900],
		[3000, 600],
		[0, 900],
	]);
	assert.equal(out.length, 2);
});

test("a blip shorter than minMs is dropped", () => {
	const seg = new Segmenter({ sampleRate: SR });
	assert.equal(
		run(seg, [
			[3000, 100],
			[0, 1000],
		]).length,
		0,
	);
});

test("pure silence never produces anything", () => {
	const seg = new Segmenter({ sampleRate: SR });
	assert.equal(run(seg, [[0, 5000]]).length, 0);
	assert.equal(seg.flush(), null);
});

test("a long monologue is cut at maxMs", () => {
	const seg = new Segmenter({ sampleRate: SR, maxMs: 2000 });
	const out = run(seg, [[3000, 5000]]);
	assert.ok(out.length >= 2, `got ${out.length}`);
	assert.ok(out.every((u) => u.durationMs <= 2100));
});

test("flush returns the pending utterance when the track ends", () => {
	const seg = new Segmenter({ sampleRate: SR });
	run(seg, [[3000, 800]]);
	const u = seg.flush();
	assert.ok(u && u.durationMs >= 800);
	assert.equal(seg.flush(), null);
});

test("toWav writes a valid 16-bit mono header", () => {
	const pcm = new Int16Array([1, -1, 2, -2]);
	const wav = toWav(pcm, SR);
	assert.equal(wav.subarray(0, 4).toString(), "RIFF");
	assert.equal(wav.subarray(8, 12).toString(), "WAVE");
	assert.equal(wav.readUInt32LE(4), 36 + 8);
	assert.equal(wav.readUInt32LE(24), SR);
	assert.equal(wav.readUInt16LE(22), 1);
	assert.equal(wav.readUInt16LE(34), 16);
	assert.equal(wav.readUInt32LE(40), 8);
	assert.equal(wav.readInt16LE(44), 1);
	assert.equal(wav.readInt16LE(46), -1);
	assert.equal(wav.length, 44 + 8);
});
