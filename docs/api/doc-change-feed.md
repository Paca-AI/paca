# Document Change Feed & Batch Read API

## Overview

Two endpoints let a client keep a local copy of a project's documents without
listing and downloading everything on each run:

- `GET /api/v1/projects/{projectId}/docs/changes` says what changed since the
  client's last call (metadata only).
- `POST /api/v1/projects/{projectId}/docs/batch-get` returns the bodies of the
  documents the client decides to fetch.

Both need `docs:read` on the project and are narrowed to the documents the
caller may read, exactly like the document list. Responses use the standard
envelope; the shapes below are the `data` object.

---

## `GET /projects/{projectId}/docs/changes?since=<cursor>`

```json
{
  "cursor": "eyJwIjoi…",
  "reset": false,
  "docs": [
    { "id": "…", "folder_id": "…", "title": "Plan", "content_hash": "9f2c…", "updated_at": "2026-11-14T10:00:00Z", "deleted": false },
    { "id": "…", "folder_id": null, "title": "", "content_hash": null, "updated_at": "2026-11-14T11:00:00Z", "deleted": true }
  ],
  "folders": null
}
```

| Field | Meaning |
|---|---|
| `cursor` | Opaque. Send it as `since` on the next call. Store it only after everything in this reply has been applied. |
| `reset` | `false`: `docs` lists the documents that changed since the cursor. `true`: `docs` is the **full list** of documents the caller can read; drop every local document that is not in it. |
| `docs` | Metadata only, oldest change first. `content_hash` is the same value the document's `ETag` carries; compare it with the hash of the local copy to decide whether to fetch the body. `folder_id` is `null` for a document outside any folder. |
| `docs[].deleted` | `true` marks a tombstone: the document was deleted. A tombstone carries the id and the time of the deletion (`updated_at`) and nothing else; `title` is empty, `folder_id` and `content_hash` are `null`. A reset never contains tombstones. |
| `folders` | `null` when no folder was created, renamed, moved or deleted since the cursor. Otherwise the whole folder tree as `{ "id", "parent_id", "name" }` (`parent_id` is `null` at the top level; the list is empty when the project has no folders). Always present on a reset. |

A call that finds nothing new returns the cursor it was given, an empty `docs`
and `folders: null`.

The feed is not paginated: an entry is about 200 bytes.

### What counts as a change

Creating, editing, renaming, moving, reordering and deleting a document.
Deleting a folder also changes each document that was in it (they move to the
top level, `folder_id: null`). A write that leaves a document as it was (the
same blocks, whatever their block ids) is not a change and does not appear.

A folder rename or move changes the path of every document below it without
changing those documents, so they are not listed: rebuild paths from `folders`.

### The reset rule

The reply is a reset (`reset: true`, full list, full folder tree) when:

- `since` is missing;
- the cursor cannot be decoded, was issued for another project, or names a
  position the project has not reached (for example after a restore from
  backup). These are never errors: the client recovers by reconciling;
- the set of documents the caller may read changed without those documents
  being written: a role or policy edit, a document or folder moved into or out
  of a folder the caller's policy denies. The cursor carries a fingerprint of
  the documents the caller could see; when the same documents no longer give
  the same fingerprint, the server cannot say which ones changed hands, so it
  sends the whole list.

**The server may answer any cursor with a reset.** A client must always handle
it, and must treat it as authoritative: a document that is missing from a reset
is gone or no longer readable, and its local copy should be removed.

### What the caller never sees

Nothing about a document the caller cannot read appears in the feed: no entry,
no tombstone, no title. A document that is created and deleted between two
calls is not reported at all. When a document the client holds moves out of the
caller's sight, the client learns only that it is no longer in the list.

Folders are not filtered: as with the folder list endpoint, every caller with
`docs:read` on the project receives the names of all its folders.

### Retention

Deleted documents are soft-deleted and are not purged, so tombstones do not
expire today: a cursor of any age still gets the deletions since it was issued.
This is not a promise for the future. If old tombstones are ever purged, a
cursor older than the purge will get a reset, which the rule above already
requires clients to handle.

Removing a local copy does not take back what a client already read. A client
that mirrors documents holds the content it fetched while it had access.

### Consistency

One call reads the project's change sequence and the documents from a single
database snapshot. A write that commits while the call is running is not in the
reply and is not skipped either: it is reported by the next call.

---

## `POST /projects/{projectId}/docs/batch-get`

```json
{ "ids": ["9b2f…", "c41a…"] }
```

At most 100 ids; more, or an id that is not a UUID, is `400 BAD_REQUEST`.

```json
{ "items": [ { "id": "…", "title": "Plan", "content": [ … ], "content_hash": "9f2c…", "…": "…" } ] }
```

Each item is the same document object `GET /projects/{projectId}/docs/{docId}`
returns, in the order the ids were given, each at most once. An id is left out,
without an error and without saying why, when the document is deleted, does not
exist, belongs to another project, or is not readable by the caller.

---

## How a client uses them

1. Call `changes` with the stored cursor (none on the first run).
2. On `reset`, remove local documents whose id is not in `docs`.
3. For each entry: `deleted` removes the local copy; a different `folder_id` or
   `title` moves it; a different `content_hash` puts it on the fetch list.
4. If `folders` is not `null`, replace the stored folder tree and recompute
   paths.
5. Fetch the bodies with `batch-get`, 100 ids at a time.
6. Store the new cursor once every step succeeded.

## Storage

Migration `000088_add_doc_change_seq.sql`: `doc_sync_state(project_id, seq,
folders_seq)` holds one change counter per project; `documents.seq` is the
counter value of the row's last write and `documents.created_seq` the value it
was inserted with. Both are maintained by database triggers, so every writer is
covered. The cursor is base64url JSON `{p: project id, s: seq, f: fingerprint}`
and is not signed: it selects a starting point and grants nothing, because every
reply is filtered by what the caller may read at the time of the call.
