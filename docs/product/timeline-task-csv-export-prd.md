# PRD: One-Click Timeline Task Export (Excel CSV)

| Field | Value |
|---|---|
| **Ticket** | PE-3 — Export All PACA Cards by Project |
| **Status** | Implemented (PR [#800](https://github.com/Paca-AI/paca/pull/800)) |
| **Branch** | `feat/timeline-instant-task-csv-export` |
| **Owner** | Prakruti V |
| **Date** | 2026-11-20 |
| **Surface** | Timeline / Backlog / Sprint board toolbars |

---

## 1. Problem statement

There was no simple way to export all cards (tasks) from a project while working on the Timeline or Board.

The existing **Settings → Export** flow already builds a full project archive ZIP asynchronously (tasks + comments + activities + docs). That path is correct for a full backup, but for day-to-day use it forced people to:

1. Leave the board
2. Open **Settings → Export**
3. Request an export
4. Wait for processing
5. Download the ZIP
6. Open `tasks.csv` inside the archive

**Expected outcome:** From the Timeline/Board, one click downloads an Excel-ready file of all project tasks with the fields teams actually track for delivery and QA.

---

## 2. Goals

- Export **all tasks in the current project** in one click from Timeline / Backlog / Sprint views.
- Produce an **Excel-openable CSV** (UTF-8 with BOM) immediately — no Settings round-trip.
- Include only the PE-3 field set (plus ID/Title for row identity).
- Gate the control with existing `project:export` permission.
- Keep the portal open while downloading (especially in Cursor Simple Browser / embedded Electron).

### Non-goals

- Replacing Settings → Export (full ZIP archive with comments/activities/docs).
- Native `.xlsx` generation (CSV opened in Excel is sufficient).
- Filtering the export to the current board view/filters only (export is **project-wide**).
- Changing backend archive export contracts.

---

## 3. User stories

1. **As a project admin**, I can click **Export** on Timeline and immediately get a CSV of every task so I can share status in Excel.
2. **As a delivery lead**, I can see Commitment, Effort, Priority, QA, and Dev Completion columns without opening each task.
3. **As a viewer without export permission**, I do not see the Export button.

---

## 4. Product behavior

### Entry point

- Labeled button: **Export** (download icon + text) in the view toolbar, next to Search and View settings.
- Visible on:
  - Timeline
  - Product Backlog
  - Sprint board
- Shown only when the actor has `project:export`.

### Click behavior

1. Button label switches to **Exporting…** and shows a spinner.
2. Client fetches all project tasks (paginated) plus project metadata and custom field definitions.
3. Client builds a CSV and triggers a browser download (`<project>-tasks-<YYYY-MM-DD>.csv`).
4. Success toast: **Exported N tasks**.
5. On failure: error toast **Could not export tasks. Please try again.**

### CSV columns

| Column | Source |
|---|---|
| ID | `{task_id_prefix}-{task_number}` |
| Title | Task title |
| Commitment | Custom field (aliases: `commitment`) |
| Estimated Effort | Custom field (aliases: estimated effort / estimate / …) |
| Priority | Built-in `importance` → None / Low / Medium / High / Critical |
| QA by | Custom field |
| QA Start Date | Custom field |
| QA End Date | Custom field |
| Actual Dev Efforts | Custom field (also matches “Actual Dev Efforts (In days)”) |
| Dev Completion Date | Custom field |

Custom fields are matched by **display name** or **field_key** (normalized, case-insensitive). Missing fields export as empty cells.

### File format

- Extension: `.csv`
- Encoding: UTF-8 with BOM (Excel-friendly)
- Formula neutralization for cells starting with `=`, `+`, `-`, `@`
- Proper CSV escaping for commas / quotes / newlines

---

## 5. Implementation summary

### Approach

**Client-side instant export** using existing task list APIs, instead of the async Settings archive pipeline.

Why:

- Instant download matches the PE-3 UX ask (“simple way to export”).
- Reuses `listAllTasks` keyset pagination already used by boards.
- Avoids waiting on Valkey worker + object storage for a tasks-only spreadsheet.
- Settings ZIP remains available for full project archives.

### Key files

| File | Role |
|---|---|
| `apps/web/src/components/projects/interactions/project-export-button.tsx` | Toolbar Export button + mutation / toast UX |
| `apps/web/src/lib/task-csv-export.ts` | Fetch all tasks, resolve columns, build/download CSV |
| `apps/web/src/lib/export-api.ts` | Safer download URL rewrite + blob download helpers (dev/local) |
| `apps/web/src/components/projects/interactions/interaction-layout.tsx` | Renders Export in the shell toolbar |
| `apps/web/src/routes/.../timeline.tsx` / `backlog.tsx` / `sprints/$sprintId.tsx` | Pass `canExport={hasProjectPermission("project:export")}` |
| `apps/web/src/i18n/locales/en/projects.json` | `layout.shell.export*` copy |
| `deploy/docker-compose.dev.yml` | Dev `STORAGE_PUBLIC_URL` / `PUBLIC_URL` default to `http://localhost:3000` |

### Flow (sequence)

```
User clicks Export
        │
        ▼
ProjectExportButton (react-query mutation)
        │
        ▼
buildProjectTasksCsv(projectId)
  ├─ getProject()
  ├─ listCustomFieldDefinitions()
  └─ listAllTasks() loop until next_cursor is null
        │
        ▼
Map rows → EXPORT_COLUMNS
        │
        ▼
downloadCsv(csv, fileName)  // Blob + a[download]
        │
        ▼
Success toast with row count
```

### Permission model

- UI gate: `project:export` (same permission as Settings → Export).
- Task reads still go through normal `tasks:read` / project membership on the list API.
- Admin role has `project:export` by default; Editor/Viewer do not.

### Local data prep used during development

Custom fields were created on local demo projects so the export columns resolve:

- Commitment (select)
- Estimated Effort (number)
- QA by (text)
- QA Start Date / QA End Date (date)
- Actual Dev Efforts (number)
- Dev Completion Date (date)

Priority uses built-in importance (no custom field).

---

## 6. Testing performed

Manual verification on local Docker Compose (`http://localhost:3000`), logged in as `admin` / `adminpassword`.

### Environments exercised

| Environment | Result |
|---|---|
| Chrome (desktop) | Export downloads CSV; Timeline stays open |
| Cursor Simple Browser / Electron | After download URL fix, portal no longer navigates away to port 80 |
| Projects `pdm`, `abc`, seeded Aurora/Nimbus/Harbor | Button visible; CSV generated |

### Bugs found & fixed during testing

1. **Icon-only control was not discoverable**  
   Fixed by switching to a labeled **Export** button (icon + text).

2. **Export felt like Settings multi-step**  
   Fixed by replacing async ZIP queue with instant client CSV download.

3. **Download navigated Cursor Simple Browser off the portal**  
   Root cause: presigned storage URLs used `http://localhost` (port 80) while the app runs on `:3000`.  
   Fixed via:
   - `STORAGE_PUBLIC_URL` / `PUBLIC_URL` defaults → `http://localhost:3000`
   - Client `resolveDownloadUrl` + blob download path in `export-api.ts`

4. **CSV contained too many columns**  
   Narrowed export to PE-3 fields only (plus ID/Title).

---

## 7. Test cases

### Functional

| ID | Case | Steps | Expected |
|---|---|---|---|
| TC-01 | Export button visible with permission | Login as Admin → open Timeline | Labeled **Export** appears in toolbar |
| TC-02 | Export button hidden without permission | Login as Viewer (no `project:export`) → Timeline | Export control not rendered |
| TC-03 | One-click download | Click **Export** | Spinner/“Exporting…” then CSV downloads without opening Settings |
| TC-04 | Toast on success | Complete export | Toast: “Exported N tasks” with correct count |
| TC-05 | Project-wide rows | Seed 15 tasks per status; export | Row count matches total project tasks (all statuses) |
| TC-06 | Column set | Open CSV in Excel | Columns are exactly: ID, Title, Commitment, Estimated Effort, Priority, QA by, QA Start Date, QA End Date, Actual Dev Efforts, Dev Completion Date |
| TC-07 | Priority mapping | Set importance Low/High/Critical on tasks; export | Priority cells show Low / High / Critical (not raw numbers) |
| TC-08 | Custom field population | Set Commitment / QA dates / Actual Dev Efforts on a task; export | Matching cells populated |
| TC-09 | Missing custom fields | Project without some custom fields; export | Headers still present; cells empty |
| TC-10 | Filename | Export from project `pdm` | File like `pdm-tasks-YYYY-MM-DD.csv` |
| TC-11 | Excel open | Open CSV in Excel / Google Sheets | UTF-8 characters readable; columns align |
| TC-12 | Formula safety | Task title starting with `=SUM(1,1)` | Cell neutralized (leading `'`) so Excel does not execute a formula |
| TC-13 | Error path | Force API failure (e.g. stop API briefly) then click Export | Error toast shown; button returns to Export |
| TC-14 | Concurrent click | Double-click Export quickly | Button disabled while pending; single coherent download |
| TC-15 | Other surfaces | Open Backlog and a Sprint board | Export button present and works the same |

### UX / regression

| ID | Case | Steps | Expected |
|---|---|---|---|
| TC-16 | Portal remains mounted | Export inside Cursor Simple Browser | App stays on Timeline; no navigation to blank `:80` host |
| TC-17 | Settings export still works | Settings → Export → request archive | Async ZIP flow unchanged |
| TC-18 | Board still usable | After export, drag/add tasks | Board interactions unaffected |

### Suggested automated coverage (follow-up)

- Unit: `buildProjectTasksCsv` column headers + priority mapping + custom-field alias matching.
- Component: `ProjectExportButton` hidden when `canExport=false`; calls download on success.
- E2E: Admin clicks Export on Timeline; file download event fired with expected name pattern.

---

## 8. Acceptance criteria

- [x] Export control is labeled and discoverable on Timeline/Board toolbar.
- [x] Users with `project:export` can download all project tasks in one click.
- [x] Users without permission do not see the control.
- [x] CSV opens in Excel and contains the PE-3 field set.
- [x] Priority is human-readable.
- [x] Custom fields resolve by name/alias when present.
- [x] Download does not kick the user out of the local portal in Cursor Simple Browser.
- [x] Changes shipped on a dedicated branch and PR against `master`.

---

## 9. Risks & follow-ups

| Risk / gap | Mitigation / follow-up |
|---|---|
| Very large projects may take longer client-side | Current page size 200 + keyset cursor; consider server-side sync CSV endpoint if projects exceed tens of thousands of tasks |
| Native `.xlsx` not produced | CSV is Excel-compatible; add SheetJS/excelize only if stakeholders require `.xlsx` |
| Export ignores current board filters | Documented as project-wide; optional “export current view” can be a later enhancement |
| Alias matching depends on field naming | Seed/document expected custom field names; aliases cover common variants |

---

## 10. References

- PR: https://github.com/Paca-AI/paca/pull/800
- Related existing feature: Settings → Export (`ExportSettings.tsx`, `/projects/{id}/exports`)
- Permission: `project:export` (`docs/guides/roles-and-policies.md`, `docs/guides/iam-authorization.md`)
