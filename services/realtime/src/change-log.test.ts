import { describe, expect, it } from "bun:test";
import { changeLogKeys, parseEntry } from "./change-log.ts";

const fields = (o: Record<string, string>) => Object.entries(o).flat();

describe("parseEntry", () => {
	const ok = {
		seq: "3",
		epoch: "1700000000000",
		id: "abc",
		type: "task.updated",
		op: "update",
		project_id: "p",
		entity_type: "task",
		entity_id: "t",
		ts: "2026-01-01T00:00:00Z",
		payload: JSON.stringify({ id: "t" }),
		authz: JSON.stringify({
			action: "tasks:read",
			resource: "project/p/task/t",
		}),
	};

	it("decodes what the API's AppendChange writes", () => {
		expect(parseEntry(fields(ok), "p")).toMatchObject({
			seq: 3,
			epoch: 1700000000000,
			type: "task.updated",
			op: "update",
			entityId: "t",
			payload: { id: "t" },
			authz: { action: "tasks:read", resource: "project/p/task/t" },
		});
	});

	it("carries the entity snapshot when there is one, and ignores a broken one", () => {
		const withEntity = parseEntry(
			fields({ ...ok, entity: JSON.stringify({ id: "t", title: "x" }) }),
			"p",
		);
		expect(withEntity?.entity).toEqual({ id: "t", title: "x" });
		expect(parseEntry(fields(ok), "p")?.entity).toBeUndefined();
		expect(
			parseEntry(fields({ ...ok, entity: "{" }), "p")?.entity,
		).toBeUndefined();
		expect(
			parseEntry(fields({ ...ok, entity: "[1]" }), "p")?.entity,
		).toBeUndefined();
	});

	it("rejects an entry it cannot deliver safely", () => {
		expect(parseEntry(fields({ ...ok, seq: "x" }), "p")).toBeNull();
		expect(parseEntry(fields({ ...ok, payload: "{" }), "p")).toBeNull();
		expect(parseEntry(fields({ ...ok, authz: "null" }), "p")).toBeNull();
	});
});

describe("changeLogKeys", () => {
	it("matches the API's keys, hash tag included", () => {
		expect(changeLogKeys("p")).toEqual({
			meta: "paca:changes:{p}:meta",
			stream: "paca:changes:{p}:stream",
		});
	});
});
