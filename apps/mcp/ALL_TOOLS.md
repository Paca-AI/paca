# All Implemented MCP Tools

This document lists all MCP tools implemented for the Paca API server.

## Tool Categories

### 1. Projects (5 tools)
- `list_projects` - List accessible projects (paged)
- `get_project` - Get details of a specific project
- `create_project` - Create a new project
- `update_project` - Update an existing project
- `delete_project` - Delete a project

### 2. Tasks (6 tools)
- `list_tasks` - List all tasks in a project
- `get_task` - Get details of a specific task
- `get_task_by_number` - Get a task by its number
- `create_task` - Create a new task
- `update_task` - Update an existing task
- `delete_task` - Delete a task

### 3. Sprints (6 tools)
- `list_sprints` - List the sprints in a project (paged)
- `get_sprint` - Get details of a specific sprint
- `create_sprint` - Create a new sprint
- `update_sprint` - Update an existing sprint
- `delete_sprint` - Delete a sprint
- `complete_sprint` - Mark a sprint as completed

### 4. Documents (5 tools)
- `list_documents` - List all documents in a project
- `get_document` - Get details of a specific document
- `create_document` - Create a new document
- `update_document` - Update an existing document
- `delete_document` - Delete a document

### Search (2 tools)
- `search_docs` - Keyword search over document titles and body text; returns path, match location and an excerpt
- `search_tasks` - Keyword search over task title, `#number` and description text; returns excerpts

### 5. Project Members (5 tools)
- `list_project_members` - List the members of a project (paged)
- `add_project_member` - Add a member to a project
- `get_my_project_permissions` - Get the IAM actions the current user may perform in a project
- `update_project_member_role` - Replace the set of roles a project member holds
- `remove_project_member` - Remove a member from a project

### 6. Project Roles (4 tools)
- `list_project_roles` - List all roles in a project
- `create_project_role` - Create a new project role
- `update_project_role` - Update an existing project role
- `delete_project_role` - Delete a project role

### 7. Task Types (5 tools)
- `list_task_types` - List all task types in a project
- `create_task_type` - Create a new task type
- `update_task_type` - Update an existing task type
- `delete_task_type` - Delete a task type
- `set_default_task_type` - Set a task type as the default for the project

### 8. Task Statuses (4 tools)
- `list_task_statuses` - List all task statuses in a project
- `create_task_status` - Create a new task status
- `update_task_status` - Update an existing task status
- `delete_task_status` - Delete a task status

### 9. Views (9 tools)
- `list_views` - List all views in a project
- `create_view` - Create a new view
- `reorder_views` - Reorder views in a project
- `get_view` - Get details of a specific view
- `update_view` - Update an existing view
- `delete_view` - Delete a view
- `list_task_positions` - List task positions in a view
- `bulk_move_tasks` - Set several tasks' positions within a view
- `move_task` - Set a task's position within a view

### 10. Custom Fields (5 tools)
- `list_custom_fields` - List all custom field definitions in a project
- `create_custom_field` - Create a new custom field definition
- `get_custom_field` - Get details of a custom field definition
- `update_custom_field` - Update a custom field definition
- `delete_custom_field` - Delete a custom field definition

### 11. Attachments (4 tools)
- `list_task_attachments` - List all attachments for a task
- `get_attachment_download_url` - Get a download URL for an attachment
- `read_task_attachment` - Download and read an attachment's contents (text or image) inline
- `delete_task_attachment` - Delete an attachment from a task

### 12. Document Folders (4 tools)
- `list_doc_folders` - List all folders in a project
- `create_doc_folder` - Create a new document folder
- `update_doc_folder` - Update a document folder
- `delete_doc_folder` - Delete a document folder

### 13. Document Snapshots (2 tools)
- `list_doc_snapshots` - List all snapshots of a document
- `get_doc_snapshot` - Get a specific document snapshot

### 14. Git (27 tools)
Available when a Git provider is installed. Credentials are minted per command from the core Git API and never stored.
- `list_repositories` - List the repositories linked to a project, with their descriptions (used to pick the right one)
- `clone_repository` - Clone a linked repository into the workspace
- `push_branch` - Push a branch of a linked repository
- `workspace_inspect` - Survey the working directory and the checkouts in it
- `workspace_prepare` - Prepare (reuse or clone, fetch, branch) a checkout
- `git_commit` - Commit changes in a checkout
- `git_push` - Push a branch of a linked repository
- `change_request_get_context` - Pull/merge request with files, review threads and CI in one call
- `change_request_get_diff` - Unified diff, whole or per file
- `review_list_threads` - Review threads with comments and resolved state
- `issue_get` / `comment_list` - Read an issue and the comments on an issue or pull request
- `checks_get` / `checks_get_logs` - CI results and the tail of a failing log
- `repo_get_file` / `repo_search_code` - Read a file or search code without a clone
- `change_request_open` / `change_request_update` - Open or edit a pull/merge request
- `comment_post` / `review_reply_thread` / `review_resolve_thread` - Comment and work threads
- `review_submit` - Review with a verdict and inline comments
- `labels_add` / `reviewers_request` / `assignees_set` - Metadata
- `checks_rerun` - Re-run failed CI (capped)
- `change_request_merge` - Merge (needs `git:merge` and the expected head SHA)


### 15. Task Activities (4 tools)
- `list_task_activities` - List activities for a task (newest first, one page per call)
- `add_task_comment` - Add a comment to a task
- `update_task_comment` - Update a task comment
- `delete_task_comment` - Delete a task comment

### 16. Task GitHub (5 tools)
- `list_task_prs` - List pull requests linked to a task
- `link_pr_to_task` - Link a pull request to a task
- `unlink_pr_from_task` - Unlink a pull request from a task
- `create_branch_for_task` - Create a branch for a task
- `list_task_branches` - List branches for a task

## Statistics

- **Total Tools**: 81 MCP tools
- **Categories**: 16 different categories
- **API Endpoints Covered**: 80+ endpoints

## API Endpoints by Category

### Projects
- GET /api/v1/projects
- POST /api/v1/projects
- GET /api/v1/projects/:projectId
- PATCH /api/v1/projects/:projectId
- DELETE /api/v1/projects/:projectId

### Tasks
- GET /api/v1/projects/:projectId/tasks
- POST /api/v1/projects/:projectId/tasks
- GET /api/v1/projects/:projectId/tasks/:taskId
- GET /api/v1/projects/:projectId/tasks/by-number/:taskNumber
- PATCH /api/v1/projects/:projectId/tasks/:taskId
- DELETE /api/v1/projects/:projectId/tasks/:taskId

### Sprints
- GET /api/v1/projects/:projectId/sprints
- POST /api/v1/projects/:projectId/sprints
- GET /api/v1/projects/:projectId/sprints/:sprintId
- PATCH /api/v1/projects/:projectId/sprints/:sprintId
- DELETE /api/v1/projects/:projectId/sprints/:sprintId
- POST /api/v1/projects/:projectId/sprints/:sprintId/complete

### Documents
- GET /api/v1/projects/:projectId/docs
- POST /api/v1/projects/:projectId/docs
- GET /api/v1/projects/:projectId/docs/:docId
- PATCH /api/v1/projects/:projectId/docs/:docId
- DELETE /api/v1/projects/:projectId/docs/:docId

### Project Members
- GET /api/v1/projects/:projectId/members
- POST /api/v1/projects/:projectId/members
- GET /api/v1/projects/:projectId/members/me/permissions
- PUT /api/v1/projects/:projectId/members/:memberId/roles
- DELETE /api/v1/projects/:projectId/members/:userId

### Project Roles
- GET /api/v1/projects/:projectId/roles
- POST /api/v1/projects/:projectId/roles
- PUT /api/v1/projects/:projectId/roles/:roleId
- DELETE /api/v1/projects/:projectId/roles/:roleId

### Task Types
- GET /api/v1/projects/:projectId/task-types
- POST /api/v1/projects/:projectId/task-types
- PATCH /api/v1/projects/:projectId/task-types/:typeId
- DELETE /api/v1/projects/:projectId/task-types/:typeId
- PUT /api/v1/projects/:projectId/task-types/:typeId/set-default

### Task Statuses
- GET /api/v1/projects/:projectId/task-statuses
- POST /api/v1/projects/:projectId/task-statuses
- PATCH /api/v1/projects/:projectId/task-statuses/:statusId
- DELETE /api/v1/projects/:projectId/task-statuses/:statusId

### Views
- GET /api/v1/projects/:projectId/views
- POST /api/v1/projects/:projectId/views
- PUT /api/v1/projects/:projectId/views/positions
- GET /api/v1/projects/:projectId/views/:viewId
- PATCH /api/v1/projects/:projectId/views/:viewId
- DELETE /api/v1/projects/:projectId/views/:viewId
- GET /api/v1/projects/:projectId/views/:viewId/task-positions
- PUT /api/v1/projects/:projectId/views/:viewId/task-positions
- PUT /api/v1/projects/:projectId/views/:viewId/task-positions/:taskId

### Custom Fields
- GET /api/v1/projects/:projectId/custom-fields
- POST /api/v1/projects/:projectId/custom-fields
- GET /api/v1/projects/:projectId/custom-fields/:fieldId
- PATCH /api/v1/projects/:projectId/custom-fields/:fieldId
- DELETE /api/v1/projects/:projectId/custom-fields/:fieldId

### Attachments
- GET /api/v1/projects/:projectId/tasks/:taskId/attachments
- GET /api/v1/projects/:projectId/tasks/:taskId/attachments/:attachmentId/download-url
- GET /api/v1/projects/:projectId/tasks/:taskId/attachments/:attachmentId/content
- DELETE /api/v1/projects/:projectId/tasks/:taskId/attachments/:attachmentId

### Document Folders
- GET /api/v1/projects/:projectId/docs/folders
- POST /api/v1/projects/:projectId/docs/folders
- PATCH /api/v1/projects/:projectId/docs/folders/:folderId
- DELETE /api/v1/projects/:projectId/docs/folders/:folderId

### Document Snapshots
- GET /api/v1/projects/:projectId/docs/:docId/snapshots
- GET /api/v1/projects/:projectId/docs/:docId/snapshots/:snapshotId

### Git

```typescript
// 1. List the repositories linked to the project
list_repositories({ projectId: "project-id" })

// 2. Prepare a checkout on a new branch
workspace_prepare({
  projectId: "project-id",
  repoId: "repository-id",
  createBranch: "fix/bug-123"
})

// 3. Commit and push
git_commit({ message: "Fix bug 123", all: true })
git_push({ projectId: "project-id", repoId: "repository-id" })
```

### Ask the user (1 tool)
- `ask_user_question` - Ask a multiple-choice question; in chat the choice returns as the user's next message, with `projectId` + `taskId`/`docId` it is posted as a task or document comment and the answer returns as a reply comment mentioning the agent

## Notes

- All tools support API key authentication
- Markdown descriptions are automatically converted to BlockNote format
- BlockNote content is automatically converted to Markdown when reading
- All operations require appropriate project permissions
- File uploads require a two-step process: initiate upload → complete upload
