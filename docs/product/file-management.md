# File management (Drive-style)

Status: backend implemented (migration 000093, API, IAM, trash purge worker); web UI not started.

## Goals

- Upload, download and manage files and folders, in a **project scope** and a **general (workspace) scope**.
- Preview docx, xlsx, pptx, pdf, images, text/code and media in the browser.
- Every project has default folders (Task attachments, Document files, Annotation screenshots) that users cannot delete, rename or move, so every uploaded file can be found and managed.
- A global **Files** page lists everything the user may read. Each project the user can reach is shown as a top-level folder, next to the workspace's own general files.

## What exists today

- `files` table plus the S3 `storage.Client` (presigned PUT/GET, multipart over 5 MiB).
- `task_attachments` joins a file to a task. Storage key is `tasks/<taskId>/<fileId>/<name>`.
- Doc folders (`doc_folders`) are a separate, docs-only tree. This design leaves them alone.
- IAM is resource-based: gates in the router (`require.Project(action)`, `require.ProjectEntity(...)`), actions in `iam/actions.go`, roots in `PlatformRootFor`, default roles in `bootstrap/defaultroles/policies`.

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
- **Task attachments** stay in `task_attachments`. Removing one from a task trashes its file; deleting the file in the manager removes the attachment from the task for good.
- **Locked files**: files inside a default folder can be previewed, downloaded and starred but not renamed or moved. Deleting a doc image or screenshot sends it to the trash (restorable for 30 days), since the owning doc or annotation may still reference it.
- **Trash** is `deleted_at` on the folder and every item in its subtree with one shared timestamp, so restore brings back exactly that subtree. A worker purges after 30 days and deletes the objects.
- **Stars and recents** (`file_stars`, `file_recents`) key on the item id, which is unique across both tables, so one query serves folders and files.
- **Avatars and workspace logos are not in `files`.** Their bytes are re-encoded and stored under `avatar_key` columns, and the handshake row is deleted. They are left out of the first release; including them later means keeping a `files` row for the processed image, shown read-only in an `Avatars` folder.


## Permissions

New actions: `files:read`, `files:write` (write covers upload, create folder, rename, move, trash, restore and permanent delete; starring is per-user and needs only read). Default roles: project Viewer and Analyst get `files:read`; Editor and Admin get both; platform `ADMIN` gets both on the general scope. Platform `USER` gets neither: `files:read`/`files:write` apply to every file in a scope, so general-scope access is granted to a role deliberately. The same migration (`000093`) adds the same to existing roles: `ADMIN` by name, and any other role that grants `docs:read` / `docs:write` gets the matching files action.

- **Project scope**: `require.Project(files:read|write)` on `project/{projectId}` for every `/projects/{projectId}/files/...` route. Limiting a role to one folder (a per-folder resource kind with a parent attribute, like docs) is not part of the first release.
- **General scope**: `files:read|write` on the platform root `file/*` (`iam.PlatformRootFor`, mirrored in the web `PLATFORM_ROOTS`, which a test keeps in sync), for the `/files/...` routes.
- **Global listing**: `GET /files/roots` is open to any signed-in caller and returns the general folder (when the caller holds `files:read` on `file/*`) plus one entry per project where they hold `files:read`, filtered in the handler like `GET /projects`.
- A node of another scope is reported as not found by the service, so an id from one project cannot be read through another project's route.

## API

Project scope under `/projects/{projectId}/files`, general scope under `/files`; handlers are shared.

| Method | Path | Purpose |
|---|---|---|
| GET | `/nodes?parent_id=&cursor=&q=` | List children (root when no parent), search by name |
| GET | `/nodes/{id}` / `/nodes/{id}/path` | Metadata / breadcrumb |
| POST | `/folders` | Create folder `{parent_id, name}` |
| PATCH | `/nodes/{id}` | Rename or move `{name?, parent_id?}` |
| DELETE | `/nodes/{id}` | Trash |
| POST | `/nodes/{id}/restore`, GET `/trash` | Trash restore / list |
| POST | `/uploads/initiate`, `/uploads/complete` | Same presigned flow as attachments, but creates the file node on complete |
| GET | `/nodes/{id}/download-url?download=true` | Presigned GET |
| GET | `/nodes/{id}/preview` | Preview descriptor (below) |

Global root: `GET /files/roots` returns `[{kind:"general"}, {kind:"project", project_id, name}...]`.

Storage key: `files/<project-or-general>/<fileId>/<safeName>`.

## Preview

Use **`@eternalheart/react-file-preview`** (MIT, v1.6.x) instead of wiring one library per format. One dependency covers images (incl. HEIC/TIFF/PSD), video and audio (Video.js), PDF (pdf.js), Word (mammoth), Excel (exceljs), PowerPoint (pptx-preview), Markdown, code (shiki), epub, 3D and more. It ships a full-screen `FilePreviewModal` with previous/next navigation, zoom and keyboard support, which matches the Drive preview overlay, so we do not build the viewer chrome ourselves. Input is `{id, name, type, url, size}`, which maps directly to a file node plus its presigned GET URL.

Integration:

- The preview overlay is opened from `FileBrowser` with the folder's previewable files as the `files` array, so arrow keys move between siblings.
- Presigned URLs are resolved lazily: the overlay asks `GET /nodes/{id}/download-url` for the current file (and the next/previous one) rather than signing the whole folder up front.
- Load it with `React.lazy` / dynamic `import()` so the roughly 14 MB package (three.js, exceljs, video.js, pdf.js) stays out of the main bundle and only loads when a preview opens.
- Self-host pdf.js: by default it fetches the worker from the unpkg CDN. Call `configurePdfjs()` with a worker, cmaps, fonts and wasm copied into the app's static files (Vite static copy), so previews work offline and under a strict CSP.
- The object store must send CORS headers for the web origin on GET, because the viewers `fetch` the presigned URL. Set this on the bucket in `deploy/` for RustFS and document it for S3.
- Files over a size cap (default 50 MiB, configurable) skip the viewer and show a download prompt. Legacy `.doc`, `.xls`, `.ppt` rely on whatever the library supports; anything it cannot render falls back to its unsupported-file view with a download button.

Risks to check in a spike before building the UI:

1. **React 19.** The package declares a peer of `react ^18`. The app is on 19.2, so the install needs a peer override and we must confirm the modal and viewers render and that framer-motion 11 works under React 19.
2. **Duplicate dependencies.** It pins `shiki ^1` and `react-markdown ^10`; the app uses `shiki ^4`. Expect two shiki copies in the lazy chunk (acceptable because it is lazy).
3. **Styling.** It ships its own `style.css`, loaded only with the lazy chunk. Check it does not leak global styles or clash with Tailwind v4 and the dark theme.
4. **Fidelity.** Office rendering is client-side and approximate for complex layouts. If that proves insufficient, the fallback is a Gotenberg (LibreOffice) PDF conversion behind `GET /nodes/{id}/preview` returning `{mode, url, content_type, size}`; the frontend then feeds the PDF to the same viewer, so nothing else changes.

Alternatives considered: assembling `pdfjs-dist` + `docx-preview` + SheetJS + `pptx-preview` ourselves (more code, more chrome to build, same fidelity limits), and server-side conversion for everything (best fidelity, but adds a heavy LibreOffice service to deploy). The library is the best starting point; the spike decides.

## Web UI

The UI follows Google Drive, not the docs tree. There is **no expandable folder tree** in a sidebar; folders are navigated by opening them in the main pane.

- Routes: `/files` (the "My Drive" equivalent: general files plus one folder per accessible project) and `/projects/$projectId/files`. Both render one `FileBrowser` component that takes a scope. The current folder is in the URL (`/files/folder/$nodeId`), so back/forward, deep links and refresh work.
- **No separate left rail.** The app sidebar gets a **Files** section, a peer of the existing Interactions and Documentation sections, with four flat items: **Files**, **Recent**, **Starred**, **Trash**. Inside a project the section appears in the project's sidebar with the same items scoped to that project; outside a project it is the global section.
- The **New** button sits at the **top right of the page header** (not in the sidebar), with a menu: New folder, File upload, Folder upload. The view toggle, details toggle and search sit next to it.
- Main pane:
  - Breadcrumb header (`My files > Project X > Specs`); each segment is clickable and a drop target, and the last segment has a dropdown with the folder's actions.
  - **Grid and list views** with a toggle. Grid shows folders in a "Folders" row above a "Files" grid of thumbnail cards; list shows Name, Owner, Last modified, Size columns with sortable headers.
  - Open a folder with a double click, open a file in the previewer with a double click, select with single click, Ctrl/Cmd and Shift click, or a rubber-band drag.
  - Right-click and a "⋮" button open the context menu: Open, Preview, Download, Rename, Move to…, Star, Details, Move to trash. The **Task attachments** folder shows a lock badge and those entries are disabled.
  - Drag files or folders from the desktop anywhere in the pane to upload; drag items onto a folder or breadcrumb segment to move them. A bottom-right upload panel shows per-file progress, cancel and retry.
  - Search bar at the top searches by name in the current scope, with type filter chips (Documents, Spreadsheets, Presentations, PDFs, Images, Videos).
  - Details side panel (name, type, size, owner, dates, location) toggled by an "i" button.
  - Preview opens as a full-screen overlay with a top bar (name, download, details, close) and previous/next across the folder's files, as in Drive.
  - Keyboard: arrows to move, Enter to open, F2 rename, Delete to trash, Ctrl+A select all.
- Entry points: the Files sidebar section, globally and per project.
- Stars and "recent" need two small extra tables (`file_stars`, `file_recents`, both per user); they are not part of the node row because they are per-user state.
- i18n: new `files` namespace in all 9 locales (parity is enforced by `locales.test.ts`).

## Delivery plan

1. **Backend core**: migration and backfill, `filedom`, repo, service, handlers, IAM actions, default roles, route-coverage and authz tests, protected folder creation on project create.
2. **Attachment sync**: task attachment flows write and remove nodes.
3. **Web browser UI**: routes, `FileBrowser`, upload and download, folders, trash, i18n.
4. **Preview**: viewers per type, lazy-loaded.
5. **Hardening**: trash purge worker, e2e spec, API docs.

## Open decisions

1. Preview approach: `@eternalheart/react-file-preview` client-side (above, decided), with Gotenberg as a fallback if fidelity is insufficient.
2. Permission model: new `files:*` actions (recommended) or reuse `docs:*`.
3. Task attachments: keep `task_attachments` as the source of truth; the file row carries the folder (decided; see Data model).
