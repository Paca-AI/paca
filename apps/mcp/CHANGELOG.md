# Changelog

All notable changes to the Paca MCP server (`@paca-ai/paca-mcp`) will be documented in this file.

The package version follows the main Paca repository release and is published on every new release.

## [Unreleased]

### Removed
- **Permission-based tool filtering** — the server no longer fetches the caller's permissions at startup or hides tools it predicts would fail. Every tool is listed unconditionally; the Paca API enforces permissions on each call and a disallowed call now surfaces as a normal "Permission denied: ..." tool error instead of the tool being missing from the list. This also fixes an unpinned caller (no `PACA_PROJECT_ID`) seeing few or no tools when their permission fetch returned little or nothing.

### Planned
- Batch operations for bulk task updates
- Pagination and filtering options for list tools
- Search functionality across projects and tasks
- Unit and integration test coverage

---

## [0.3.1] — 2026-06-05

Full changelog: [github.com/Paca-AI/paca/releases/tag/v0.3.1](https://github.com/Paca-AI/paca/releases/tag/v0.3.1)

No MCP-specific changes. Released in sync with the main repository.

---

## [0.3.0] — 2026-06-04

Full changelog: [github.com/Paca-AI/paca/releases/tag/v0.3.0](https://github.com/Paca-AI/paca/releases/tag/v0.3.0)

No MCP-specific changes. Released in sync with the main repository (AI agent integration).

---

## [0.2.0] — 2026-05-22

Full changelog: [github.com/Paca-AI/paca/releases/tag/v0.2.0](https://github.com/Paca-AI/paca/releases/tag/v0.2.0)

### Added
- **Permission-based tool filtering** — tools are filtered at startup based on the authenticated user's or agent's permissions. Unauthorized tools are hidden rather than returning errors.
- **Agent mode** — set `PACA_AGENT_ID` + `PACA_PROJECT_ID` to impersonate a project agent using the server's global `AGENT_API_KEY`.
- **User single-project mode** — set `PACA_PROJECT_ID` without `PACA_AGENT_ID` to scope all tools to a single project.
- **Project members** (5 tools): `list_project_members`, `add_project_member`, `get_my_project_permissions`, `update_project_member_role`, `remove_project_member`.
- **Project roles** (4 tools): `list_project_roles`, `create_project_role`, `update_project_role`, `delete_project_role`.
- **Task types** (5 tools): `list_task_types`, `create_task_type`, `update_task_type`, `delete_task_type`, `set_default_task_type`.
- **Task statuses** (4 tools): `list_task_statuses`, `create_task_status`, `update_task_status`, `delete_task_status`.
- **Views** (9 tools): `list_views`, `create_view`, `reorder_views`, `get_view`, `update_view`, `delete_view`, `list_task_positions`, `bulk_move_tasks`, `move_task`.
- **Custom fields** (5 tools): `list_custom_fields`, `create_custom_field`, `get_custom_field`, `update_custom_field`, `delete_custom_field`.
- **Attachments** (3 tools): `list_task_attachments`, `get_attachment_download_url`, `delete_task_attachment`.
- **Document folders** (4 tools): `list_doc_folders`, `create_doc_folder`, `update_doc_folder`, `delete_doc_folder`.
- **Document snapshots** (2 tools): `list_doc_snapshots`, `get_doc_snapshot`.
- **GitHub integration** (7 tools): `get_github_integration`, `set_github_token`, `delete_github_token`, `list_github_repositories`, `list_linked_github_repos`, `link_github_repository`, `unlink_github_repository`.
- **Task GitHub** (5 tools): `list_task_prs`, `link_pr_to_task`, `unlink_pr_from_task`, `create_branch_for_task`, `list_task_branches`.
- **Task activities** (4 tools): `list_task_activities`, `add_task_comment`, `update_task_comment`, `delete_task_comment`.
- **Plugin tools** — plugins that declare an `mcp.remoteEntryUrl` in their manifest are loaded dynamically at startup. Their tools appear alongside core tools with no distinction from the client's perspective.

### Changed
- Total tool count: **23 → 81 tools** across **16 categories**.
- Task detail enriched with additional fields (story points, parent task, tags, custom fields).

---

## [0.1.1] — 2026-04-28

Full changelog: [github.com/Paca-AI/paca/releases/tag/v0.1.1](https://github.com/Paca-AI/paca/releases/tag/v0.1.1)

### Fixed
- Project tool descriptions clarified to specify that `projectId` parameters expect a UUID, not a project name.

---

## [0.1.0] — 2026-04-28

Full changelog: [github.com/Paca-AI/paca/releases/tag/v0.1.0](https://github.com/Paca-AI/paca/releases/tag/v0.1.0)

### Added
- Initial release of `@paca-ai/paca-mcp`.
- API key authentication via `X-API-Key` header (`PACA_API_KEY` environment variable).
- **Project management** (5 tools): `list_projects`, `get_project`, `create_project`, `update_project`, `delete_project`.
- **Task management** (6 tools): `list_tasks`, `get_task`, `get_task_by_number`, `create_task`, `update_task`, `delete_task`.
- **Sprint management** (6 tools): `list_sprints`, `get_sprint`, `create_sprint`, `update_sprint`, `delete_sprint`, `complete_sprint`.
- **Document management** (5 tools): `list_documents`, `get_document`, `create_document`, `update_document`, `delete_document`.
- Automatic BlockNote ↔ Markdown conversion for task descriptions and document content.
- Modular architecture: `types`, `api`, `tools`, `utils`, `server`, `index` layers.
