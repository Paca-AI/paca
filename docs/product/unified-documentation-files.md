# Unified Documentation and Files (design proposal)

Status: implemented (one migration, `000096`); follow-ups listed at the end. Replaces the interim `doc_folder_files` link table (migration `000096`), which exists only because the two systems have separate folder trees.

## Goal

One folder hierarchy per project that holds **documents and files together**. Documentation becomes a top-level folder of the project's Files; its subfolders are the user-created documentation folders. The docs sidebar tree stays, and the Files page also shows documents.

## Why the interim design is not enough

| Problem with `doc_folders` + link table | Effect |
|---|---|
| Two folder trees | A file has a folder in Files *and* a link into Documentation; moving, deleting and "where is it?" have two answers |
| Two permission models | Docs gate on per-folder `docs:*`, Files on project-wide `files:*`; the link routes need both |
| Two trash models | Doc folder delete re-roots its documents; Files trashes a subtree for 30 days |
| Files page cannot show documents | Documentation is invisible in Files search, Recent, Starred |

## Decisions

1. **Merge the folder tables.** `doc_folders` rows move into `file_folders` **keeping their ids**, so `documents.folder_id`, MCP mirror paths, links and the change feed stay valid. `documents.folder_id` then references `file_folders`.
2. **Documents stay documents.** The `documents` table (BlockNote content, snapshots, comments, realtime sync) is not converted into `files` rows. `file_items` gains a third branch, `kind = 'doc'` (`name = title`), so listing, search, sort and paging stay one query.
3. **A system folder `documentation`** (`system_key = 'documentation'`, one per project, created by the existing project trigger and backfilled) is the parent of every former root-level doc folder. It cannot be deleted or moved. Users may create folders, documents and files inside it, unlike the other system folders.
4. **A file has exactly one folder.** `files.folder_id` is the single location; "show a file in several places" is dropped. The link table and its routes are removed. (Keeping "also appears in…" is the one capability lost; a later `file_shortcuts` table can add Drive-style shortcuts if it is missed.)
5. **Docs API contract is unchanged.** `/docs/folders` and `/docs` keep their shapes. A `null` parent means "directly under Documentation": the service maps it to the system folder id. MCP, by-path, export and the sync protocol keep working untouched.

## Permissions

Today docs support per-folder rules (a `doc` resource with a folder-parent attribute), Files do not (`docs/product/file-management.md`: "Per-folder permissions: not supported"). Rule for the merged tree, decided by where the node lives:

- Node **inside the Documentation subtree**: gated by `docs:read` / `docs:write` on the folder or document resource, as now. Files there follow the folder's rule, so no second `files:*` check and no "can read the folder but not its file" gap.
- Node **elsewhere**: gated by project-wide `files:read` / `files:write`, as now.

The router gate loads the node's ancestor chain once (the attribute loaders already walk `doc_folders` ancestors; they are re-pointed at `file_folders`) and picks the action family. Handlers and services stay free of authorization, per the project rule.

Moving a node across the boundary (into or out of Documentation) needs write on both sides.

## Trash and deletion

Documents already soft-delete with `deleted_at`. Folder trash is unified: trashing a documentation folder stamps the subtree (folders, documents, files) with one `deleted_at`; restore brings back the subtree; the purge worker removes it after 30 days. **Behavior change:** deleting a doc folder no longer re-roots its documents to the top level. Deleting a file keeps the existing rules (files used by a task, document or annotation are deleted for good after confirmation).

## Ordering and names

Docs order manually (`position`) and by-path resolution needs a predictable name, while Files order by name and allow duplicate names. `file_folders` gains `position`. Inside the Documentation subtree the existing uniqueness and manual-order rules of docs keep applying (enforced by the docs service, not by a constraint on `file_folders`); Files elsewhere keep Drive behavior.

## Change feed and sync

`doc_folders_bump_seq` moves to `file_folders`, firing only for folders in the Documentation subtree so the feed and MCP mirror see exactly the folders they see today. Files do not enter the feed in this phase (the mirror is Markdown only); a `file` entry kind can be added later.

## API

New listing for the browser and the sidebar, one request for folders, documents and files: `GET /projects/{id}/files/nodes?parent_id=` now returns `kind: "doc"` items too (with `doc_id`, `title`, no size). The sidebar tree reads the same endpoint, so one ordering and one paging scheme (offset) replaces the two it uses today. Creating a document from Files calls the existing `POST /docs` with the folder id.

## Web UI

- **Sidebar tree** stays. It lists folders, documents and files of the Documentation subtree from the unified endpoint, with the file rows and add menu already built (`doc-folder-files.tsx`), but linking is replaced by plain upload into the folder (the file *is* in the folder).
- **Files page** shows Documentation as a locked top-level folder; documents appear with a document icon and open the editor (`/projects/$projectId/docs/$docId`); **New** gains *Document*. Search, Recent and Starred include documents.
- Drag between the sidebar tree and the Files page works because both move the same nodes.

## Phase 1 as built

Differences from the plan above, all to keep the existing docs code and tests untouched:

- `doc_folders` is kept as an **updatable view** over the Documentation subtree (`INSTEAD OF` triggers write `file_folders`), instead of re-pointing every docs query. It is retired in a later phase once the docs repository reads `file_folders` directly.
- `file_folders.area = 'docs'` marks the subtree (kept current by triggers when a folder moves), so phase 2's gate can pick `docs:*` vs `files:*` without walking ancestors. `file_items` exposes `area`.
- Documents with `folder_id IS NULL` still mean "directly under Documentation"; phase 2 maps them in the unified listing.
- The file manager treats docs-area folders as locked (no rename, move or trash from Files) until phase 2 teaches it to manage them.
- Deleting a doc folder keeps today's contract (documents re-root, subfolders go) rather than trashing the subtree, because documents have no trash yet. Files inside are **trashed** (they appear in the Files trash at the top level) instead of being deleted with the folder row. Full subtree trashing moves to phase 2.
- `doc_folder_files` (the interim link table) is kept, with its foreign key re-pointed to `file_folders`, until the sidebar moves to the unified listing in phase 4.
- The old `000088` migration can no longer be replayed verbatim (its trigger statements target a view); the replay test strips them.

## Phase 2 as built

- **`file_items` has a `doc` kind** (migration `000096`): live documents of the docs tree (no notes, no soft-deleted ones), parented by their folder or, with none, by the project's Documentation folder. `file_items.area` is `docs` for everything in the Documentation subtree, files included. The file manager API now returns `kind: "doc"` nodes when you list a Documentation folder.
- **Documents are read-only in the file manager.** Rename, move, trash and download of a `doc` node (and of a documentation folder) are refused with `FILE_NODE_LOCKED` / `FILE_NODE_NOT_FILE`; the docs editor owns them. A *file* inside Documentation stays an ordinary file.
- **The gate is "both".** Every project file route keeps `files:read|write` and, when the node it names is in the Documentation subtree, also requires `docs:read|write` on the project (`RequireFileDocsArea`, declared in the router). The nodes checked are the one in the URL and the parent or file in the body or query, so moving a node into or out of Documentation needs the docs action for both ends; a request that touches nothing in Documentation is unaffected. This replaces the "pick one action family" idea: it keeps the router the only place that decides and works for per-document rules later.
- **Whole-scope lists skip Documentation.** Search, Starred, Recent and Trash never return docs-area nodes, because those lists cannot ask the docs action per node. Search Documentation with the docs search, or by browsing the folder. Files that were in a deleted doc folder land in the Trash at the top level, so they show there.
- **Web UI** only learns the new kind: the Files page and the file picker hide documents until phase 3.
- **Not done in this phase:** trashing a documentation folder's whole subtree (documents still have no trash), and per-document `docs:*` rules for documents listed in Files (the folder-level gate applies; opening a document still goes through its own `ProjectEntity` gate).

## Phase 3 as built

- **The Files page shows Documentation.** The Documentation folder, its subfolders and its documents are listed in grid and list views. A document opens in the docs editor (`/projects/{id}/docs/{docId}`); its menu offers Open and Details only (no star, rename, move, delete or download). Documentation folders cannot be renamed, moved or trashed from Files.
- **Creating in Documentation.** In the Documentation folder or one of its subfolders, **New** offers *Document* (created in that folder; in the Documentation folder itself it is a top-level document), *New folder*, and the two uploads. The API accepts folders and files in the Documentation folder; a folder made from Files is a docs folder for the docs API (same table, `area = docs`). Creating there needs `docs:write` as well as `files:write`; the New menu is hidden without both.
- **Moving files in and out.** Files can be dragged or moved into documentation folders (the move dialog lists the Documentation folder and its subfolders); the server requires `docs:write` for that end.
- **API.** File nodes carry `area` (`docs` in Documentation) and a locked flag that is true for documents and documentation folders. Files directly in the Documentation folder are ordinary files.

## Phase 4 as built

- **The link table is gone.** The interim link table never shipped, so no migration creates or converts it. The link API, its service, repository, error codes and web client are removed.
- **Sidebar files come from the file manager.** A documentation folder's files are the folder's file nodes (`GET /files/nodes?parent_id=`), with preview, download, *Details*, *Show in Files*, *Move to…* (a docs-folder picker whose top is the Documentation folder) and *Move to trash*. **Upload file** stores straight into the folder; **Add from project files** moves an unmanaged file in.
- **Relationships moved to the file manager:** `GET .../files/nodes/{nodeId}/relations` (task and referencing documents), shown in the Files details sheet, which the sidebar reuses. The docs gate applies to it like any node read.
- **Deliberate deviation:** the sidebar's folders and documents still use the docs API. They need manual order and cursor paging, which the offset-paged unified listing does not offer; only files moved to the unified listing. Folder and document rows can follow later without changing the data model.

## Migrations

All of this ships as one migration, `000096_documentation_in_files.sql`: the folder merge, the `doc_folders` view and its triggers, the change-feed trigger move, `documents.trashed_at`, the `file_items` view with documents, and the trash-on-delete trigger. The interim migrations written while building it (a link table and several follow-ups) were folded into it; a development database that applied them needs to be recreated.

## Migration plan (original, before the work was folded together)

1. `000097`: add `file_folders.position`; add `documentation` to the `system_key` check and unique index; create the folder per project; copy `doc_folders` into `file_folders` (same ids, root folders get the Documentation parent); repoint `documents.folder_id` FK; convert `doc_folder_files` rows into `files.folder_id` (a file linked to several folders keeps its oldest link; the rest are reported in the migration log); move the seq trigger; drop `doc_folders` and `doc_folder_files`.
2. Rebuild the IAM attribute loaders (`iam_attribute_loaders.go`, `iam_scope.go`) on `file_folders`.
3. The migration is one-way; ship it with a database backup note and an integration test over a seeded legacy dataset.

## Phases

| Phase | Scope | Verifiable by |
|---|---|---|
| 1 | Migration, repository and service re-pointing, docs API facade, IAM loaders, trash unification | Existing doc/by-path/changes/MCP tests unchanged and green; new migration test |
| 2 | `kind: doc` in `file_items`, unified listing, gate that picks docs vs files action | Router authorization tests; pg tests |
| 3 | Files page shows documents; New → Document | e2e `apps/e2e/tests/files` |
| 4 | Sidebar tree on the unified endpoint; drop link table UI | e2e docs specs |
| 5 | Docs (`file-management.md`, `database-schema.md`, MCP guide), cleanup of dead routes | Review |

## Risks

- **Per-folder docs permissions vs. Files**: the gate that picks an action family by ancestry is the riskiest piece; it needs an exhaustive authorization test matrix.
- **Change feed correctness**: moving the trigger must keep `seq` ordering and the lock order documented in `lockDocSeq` (counter first, then rows).
- **Behavior changes** (doc folder delete trashes instead of re-rooting; one folder per file) must be called out in release notes.
- **Scale of phase 1**: about 30 Go files reference `doc_folders` or its service; the facade approach keeps their contracts, but the diff is still large.

## Documents as Markdown files (second iteration)

Direction: a document should behave like a file, downloads as Markdown, and uploaded Markdown becomes a document. Decisions:

- **Source of truth stays the `documents` table** (BlockNote JSON). A document is presented as the file `<title>.md` (`text/markdown`); realtime editing, snapshots, comments and mentions are unchanged. The `.md` is produced on download and consumed on upload.
- **Only Markdown uploaded into Documentation becomes a document.** Elsewhere in Files a `.md` is an ordinary file, because a document can only live in the Documentation tree.
- **The web app converts on upload** with BlockNote's own Markdown parser (same result as the editor and MCP). A raw API upload of a `.md` stays a plain file.
- **Round trip.** A download is `# <title>` followed by the body; an upload whose first block is a level-1 heading takes it as the title and drops it from the body, any other file is titled by its name.

### Phase 6 as built (download and import)

- `GET /projects/{id}/docs/{docId}/markdown` (gate `docs:read` on the document) returns `text/markdown` as an attachment named `<title>.md`. The renderer is `docdom.RenderMarkdown`, shared with the project zip export.
- In the file tree a document is named `<title>.md` with content type `text/markdown`.
- Files page and docs sidebar: **Download** on a document; **Download as Markdown** in the editor header; uploading or dropping `.md` files into Documentation (also inside dropped folders) creates documents instead of stored files.

### Phase 7 as built (file operations)

- **Rename and move** a document from Files: the UI calls the docs API (`PATCH /docs/{id}` with `title`, `.md` stripped, or `folder_id`), so `docs:write` on the document and the folder-attribute rules apply exactly as in the editor. The top of Documentation is "no folder" there; a target outside Documentation is refused in the UI. The file routes still refuse to rename, move or trash a document, and its node is reported as not locked so the UI offers the actions.
- **Per-document gate.** When a file route names a document, `RequireFileDocsArea` authorizes `docs:read|write` on `project/{id}/doc/{docId}` (not just the project), so a Deny on that document or a role limited to some folders applies to Files too. Folders and files inside Documentation keep the project-wide docs check.
- **Star and Recent.** Documents can be starred like files; opening one from Files records it in Recent through `POST /files/nodes/{id}/open`.
- **Search, Starred and Recent include documents the caller may read.** The file handler attaches the caller's document scope (the same one the docs list uses) and the repository applies it inside the query to the document rows; Documentation's folders and files follow the project-wide docs access. Without a scope (or with one that allows nothing) Documentation stays out of these lists, as before.
- **Not in this phase:** trash and restore of documents (phase 8). Opening a document from the docs sidebar does not record a Recent entry.

### Phase 8 as built (trash)

- **Documents and documentation folders go to the Files trash** with the same 30-day retention and purge worker as files. Trashing a folder takes its whole subtree (subfolders, documents, files) with one shared timestamp; restoring it brings the subtree back together. A document restored while its folder is still trashed returns to the top of Documentation; a restored documentation folder stays inside Documentation.
- **Deleting from the docs app is the same operation.** `DELETE /docs/{id}` and `DELETE /docs/folders/{id}` now trash (restorable from Files), so deleting a documentation folder no longer re-roots its documents: they are trashed with it, and the change feed reports them as deletions. The documentation folder itself, like the other default folders, cannot be trashed.
- **Data model** (migration `000096`): `documents.trashed_at` marks the deletions a user can undo; `deleted_at` stays the soft-delete and the feed's tombstone. Documents deleted before the migration, and the notes a deleted view takes with it, have no `trashed_at`, so they are not listed and never purged. `file_items` exposes trashed documents with `deleted_at = trashed_at`; `doc_folders` still hides trashed folders.
- **Purge keeps the tombstone.** When retention ends, or on *Delete forever*, a document's content, snapshots, stars and recents are erased but its row stays (the sync feed needs it); a purged folder subtree's files are deleted for good with their bytes.
- **Permissions.** Trash and restore of a document go through the file routes' per-document gate; Files' trash list applies the caller's document scope like search does.

## Follow-ups

- Retire the `doc_folders` view: point the docs repository and the IAM loaders at `file_folders` directly.
- Sidebar folders and documents on the unified listing (needs manual order and cursor paging there).
- Per-document `docs:*` rules for documents listed in Files; Search, Starred and Recent across Documentation.
- Optional Drive-style shortcuts if showing one file in several folders is missed.

## Decisions taken while building

1. One file lives in one folder; showing it elsewhere would be a shortcut (not built).
2. Deleting a doc folder trashes it with its subfolders, documents and files; restoring the folder brings the whole subtree back.
3. The Documentation folder is locked (not renamable); its subfolders rename and move through the docs sidebar.

## Known limits

- **Trashing a folder trashes everything in it, including documents the caller
  could not open.** The gate is `docs:write` on the project; a per-document Deny
  applies to acting on that document, not to the folder it sits in. Restoring the
  folder brings them all back.
- **File relations scan document content.** `GET /files/nodes/{id}/relations`
  matches the file's id against each document's content with no index. Add a
  trigram index or a referenced-files table if projects grow large.
- **The `doc_folders` view overwrites every column on update.** Its `INSTEAD OF
  UPDATE` trigger copies `parent_id`, `name`, `position` and `updated_at` from
  the new row, so a caller must send the full row. Retire the view when the docs
  repository reads `file_folders` directly.
- **`/relations` also needs `tasks:read`,** because it names the task a file is
  attached to.
