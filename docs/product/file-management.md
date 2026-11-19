# File management (Drive-style)

Status: implemented — migration `000093`, the API, IAM, the trash purge worker and the web UI (Files pages, preview, task and description integrations).

## Goals

- Upload, download and manage files and folders, in a **project scope** and a **general (workspace) scope**.
- Preview docx, xlsx, pptx, pdf, images, text/code and media in the browser.
- Every project has default folders (Task attachments, Document files, Annotation screenshots) that users cannot delete, rename or move, so every uploaded file can be found and managed.
- A global **Files** page lists everything the user may read. Each project the user can reach is shown as a top-level folder, next to the workspace's own general files.

## Building blocks

- The `files` table and the S3 `storage.Client` (presigned PUT/GET, multipart from 5 MiB) hold every upload: user uploads, task attachments, doc images and annotation screenshots.
- `task_attachments` joins a file to a task. Storage key is `tasks/<taskId>/<fileId>/<name>`.
- Doc folders (`doc_folders`) are a separate, docs-only tree and are left alone.
- IAM is resource-based: gates in the router (`require.Project(action)`, `require.Global(action)`), actions in `iam/actions.go`, roots in `PlatformRootFor`, default roles in `bootstrap/defaultroles/policies`.

## Data model (migration `000093_file_management.sql`)

**Every uploaded file is a manageable file.** The `files` table already holds all of them (user uploads, task attachments, doc images, annotation screenshots), so it gains the manager columns instead of a parallel node table. Only folders are new.

```sql
CREATE TABLE file_folders (
    id UUID PRIMARY KEY, project_id UUID REFERENCES projects ON DELETE CASCADE, -- NULL = general scope
    parent_id UUID REFERENCES file_folders ON DELETE CASCADE,                   -- NULL = scope root
    name TEXT NOT NULL, system_key TEXT,  -- task_attachments | task | docs | annotations
    created_by, created_at, updated_at, deleted_at, deleted_by);

ALTER TABLE files ADD COLUMN project_id UUID REFERENCES projects ON DELETE CASCADE,
                  ADD COLUMN folder_id  UUID REFERENCES file_folders ON DELETE CASCADE,
                  ADD COLUMN source TEXT NOT NULL DEFAULT 'other',  -- upload | task_attachment | doc | annotation | other
                  ADD COLUMN deleted_at TIMESTAMPTZ, ADD COLUMN deleted_by UUID;
```

- A **view `file_items`** unions folders and uploaded files into one node shape (`id, kind, project_id, parent_id, name, ...`), so listing, search, sort and paging are a single query. Writes go to the right table.
- A file is **visible in the manager** when it is uploaded and either `source = 'upload'` or it has a `folder_id`. Pending uploads and unclassified rows stay hidden.
- Names are **not unique** within a folder (Drive allows duplicates), which also suits doc images that are all called `image.png`.
- **Default folders** per project, created by a trigger on `projects` (and backfilled): `Task attachments` (with one subfolder per task, `PACA-12`), `Document files`, `Annotation screenshots`. They carry a `system_key`, are locked (no rename, move or delete) and nothing can be created or moved into them.
- A `BEFORE INSERT` trigger on `files` classifies a new row from its storage-key prefix (`tasks/`, `docs/`, `annotations/`, `files/`) and, for tasks and docs, resolves the project and default folder, so the existing upload services keep working unchanged. Annotation screenshots get their folder when attached to an annotation (trigger on `page_annotations`). Existing rows are backfilled.
- **Task attachments** stay in `task_attachments`. Removing one from a task trashes its file, provided the file has a folder; a row without one (its task no longer exists) is not in `file_items`, so it is left alone rather than stamped as trashed where neither the trash list nor the purge could reach it.
- **Locked files**: files inside a default folder can be previewed, downloaded and starred but not renamed or moved.
- **Deleting files that something else uses.** A task attachment, a document file or an annotation screenshot is referenced by its owner (`task_attachments`, the document content, `page_annotations.screenshot_file_id`). Deleting one from the manager deletes it **for good**, and the web UI asks for confirmation first, naming what it is used by. It is not trashed: a trash entry would only postpone, silently, the moment the owner loses the file (when the 30-day retention ends). A task attachment is unlinked from its task first; a screenshot lets go of its annotation and task through the foreign keys on `files`. Plain uploads go to the trash as usual.
- **Trash** is `deleted_at` on the folder and every item in its subtree with one shared timestamp, so restore brings back exactly that subtree. A worker purges after 30 days and deletes the objects.
- **Stars and recents** (`file_stars`, `file_recents`) are per-user and key on the item id, which is unique across both tables, so one query serves folders and files.
- **Avatars and workspace logos are not in `files`.** Their bytes are re-encoded and stored under `avatar_key` columns, and the handshake row is deleted. They are left out; including them later means keeping a `files` row for the processed image, shown read-only in an `Avatars` folder.

## Permissions

New actions: `files:read`, `files:write` (write covers upload, create folder, rename, move, trash, restore and permanent delete; starring is per-user and needs only read). Default roles: project Viewer and Analyst get `files:read`; Editor and Admin get both; platform `ADMIN` gets both on the general scope. Platform `USER` gets neither: `files:read`/`files:write` apply to every file in a scope, so general-scope access is granted to a role deliberately. The same migration adds the actions to existing roles (seeding never rewrites the policy of a role that already exists): `ADMIN` by name, and any other role that grants `docs:read` / `docs:write` in an Allow statement **confined to projects** (every resource under `project/`) gets the matching files action in that statement. A statement that can match `file/*`, such as resource `*`, is left alone, so the general scope is never derived from a docs grant.

- **Project scope**: `require.Project(files:read|write)` on `project/{projectId}` for every `/projects/{projectId}/files/...` route. Limiting a role to one folder (a per-folder resource kind with a parent attribute, like docs) is not supported.
- **General scope**: `files:read|write` on the platform root `file/*` (`iam.PlatformRootFor`, mirrored in the web `PLATFORM_ROOTS`, which a test keeps in sync), for the `/files/...` routes.
- **Global listing**: `GET /files/roots` is open to any signed-in caller and returns the general folder (when the caller holds `files:read` on `file/*`) plus one entry per project where they hold `files:read`, filtered in the handler like `GET /projects`.
- A node of another scope is reported as not found by the service, so an id from one project cannot be read through another project's route.

## API

Project scope under `/projects/{projectId}/files`, general scope under `/files`; handlers are shared. Lists take `page_size` (default 100, at most 500) and `offset` and answer `{items, total_count, page_size, offset, has_more}`.

| Method | Path | Purpose |
|---|---|---|
| GET | `/nodes?parent_id=&q=&type=&sort=&order=` | List children (the scope root when no `parent_id`), or search the whole scope by name |
| GET | `/nodes/{id}`, `/nodes/{id}/path` | Metadata / breadcrumb |
| POST | `/folders` | Create a folder `{parent_id, name}` |
| PATCH | `/nodes/{id}` | Rename and/or move `{name?, move?, parent_id?}` (`move` with a null `parent_id` moves to the root) |
| DELETE | `/nodes/{id}` | Trash, or delete for good for files that something else uses |
| POST | `/nodes/{id}/restore` | Restore from the trash |
| DELETE | `/nodes/{id}/permanent` | Delete a trashed item for good |
| GET | `/trash`, `/starred`, `/recent` | The trashed roots, the caller's starred items, the caller's recently opened files |
| PUT | `/nodes/{id}/star` | Star or unstar `{starred}` (per user) |
| POST | `/uploads/initiate`, `/uploads/complete` | The same presigned flow as attachments; the file appears in the folder given at initiate once complete |
| GET | `/nodes/{id}/download-url?download=true` | Presigned GET, also recorded in the caller's recents |

Global: `GET /files/roots` returns `[{kind:"general"}, {kind:"project", project_id, name}...]`.

Storage key: `files/<project-or-general>/<fileId>/<safeName>`.

## Preview

The viewer is **`@eternalheart/react-file-preview`** (MIT). One dependency covers images, video and audio, PDF (pdf.js), Word (mammoth), Excel (exceljs), PowerPoint (pptx-preview), Markdown, code and more, with a full-screen `FilePreviewModal` (zoom, keyboard, download). It is used in [`file-preview.tsx`](../../apps/web/src/components/files/file-preview.tsx).

- The viewer takes `{id, name, type, url, size}`. The presigned URL is fetched for the file being shown only (not the whole folder), through a `resolveUrl` the caller can supply. The file manager uses `download-url`; task attachments and description files use the task attachment API, so access follows the task and not `files:read`.
- The package (about 14 MB with three.js, exceljs, video.js, pdf.js) is loaded with `React.lazy` only when a preview opens.
- The PDF worker is served from our own origin (`pdfjs-dist` is pinned to the library's version), so PDFs work offline and under a strict CSP. pdf.js' cmaps and fonts still come from its default CDN.
- The object store must allow the web origin on GET (CORS), because the viewers `fetch` the presigned URL.
- Files over 50 MiB skip the viewer and show a download prompt. Formats the library cannot render fall back to its unsupported-file view with a download button.
- Office documents are rendered client-side and are approximate for complex layouts. The fallback, if that proves insufficient, is a server-side PDF conversion (Gotenberg) behind the same URL resolver; nothing else would change.

## Web UI

The UI follows Google Drive, not the docs tree. There is **no folder tree** in a sidebar; folders are opened in the main pane.

- Routes: `/files` (general files plus one folder per accessible project) and `/projects/$projectId/files`, each with `recent`, `starred` and `trash`. All render one `FilesPage` that takes a scope. The folder is in the URL (`?folder=<id>`, plus `?scope=general|<projectId>` on the global page), so back/forward, links and refresh work.
- **Files section in the app sidebar**, a peer of Interactions and Documentation, with **Files**, **Recent**, **Starred** and **Trash**: per project inside a project, global outside one. There is no separate left rail.
- **New** (New folder, File upload, Folder upload) is at the top right of the page header, next to search, the type filter and the list/grid toggle.
- Main pane:
  - Breadcrumb header (`Files > Project X > Specs`); each segment is clickable and a drop target.
  - **Grid and list views**; list has sortable Name, Owner, Last modified, Size columns.
  - Double click opens a folder or the preview; single click, Ctrl/Cmd and Shift click select. Default folders show a lock.
  - Right-click and the "⋮" button open the context menu: Open or Preview, Download, Star, Rename, Move to…, Details (a side sheet), Move to trash (Delete for files that something else uses, with a confirmation).
  - Drop files or folders from the desktop to upload into the current folder; drag items onto a folder or breadcrumb segment to move them. Upload progress is shown in the shared toast (one loading toast per batch, with Cancel).
  - Name search across the scope with a file-type filter (documents, spreadsheets, presentations, PDFs, images, video, audio, archives).
  - Full-screen preview with previous/next across the folder's files.
  - Keyboard: Ctrl+A, Delete, F2, Enter, Escape.
- Recent, Starred and Trash show up to 100 items per scope (no paging). The global versions merge one request per project the caller can read.
- Strings are in the `files` namespace in all nine locales.

## Integrations

- **Task detail.** Clicking a task attachment, an inline image or a file block in the description opens the preview instead of downloading, with previous/next across the description's media. Files from the project's Files are stored in a block as `files://{projectId}/{fileId}` and resolved to a fresh URL when shown (`attachment://` is the existing scheme for task attachments).
- **Description editor.** The file panel of image, video, audio and file blocks is replaced by one that matches the app: **Upload**, **From files** (a compact browser of the project's Files, narrowed to the block's type; shown only with `files:read`) and **Embed link**.

## Not included

- Avatars and workspace logos (see above).
- Per-folder permissions.
- Rubber-band selection, retry of a failed upload, and paging of Recent, Starred and Trash.
- Choosing from Files in editors other than the task description.
