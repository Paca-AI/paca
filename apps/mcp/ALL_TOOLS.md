# All Implemented MCP Tools

This document lists all MCP tools implemented for the Paca API server.

## Tool Categories

### 1. Projects (5 tools)
- `list_projects` - List all accessible projects
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
- `list_sprints` - List all sprints in a project
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

### 5. Project Members (5 tools)
- `list_project_members` - List all members of a project
- `add_project_member` - Add a member to a project
- `get_my_project_permissions` - Get the current user's permissions in a project
- `update_project_member_role` - Update a project member's role
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
- `bulk_move_tasks` - Bulk move tasks in a view
- `move_task` - Move a task within a view

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

### 14. GitHub Integration (7 tools)
- `get_github_integration` - Get GitHub integration status for a project
- `set_github_token` - Set GitHub token for a project
- `delete_github_token` - Delete GitHub token for a project
- `list_github_repositories` - List available GitHub repositories
- `list_linked_github_repos` - List linked GitHub repositories
- `link_github_repository` - Link a GitHub repository to a project
- `unlink_github_repository` - Unlink a GitHub repository from a project

### 15. Task Activities (4 tools)
- `list_task_activities` - List all activities for a task
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
- PATCH /api/v1/projects/:projectId/members/:userId
- DELETE /api/v1/projects/:projectId/members/:userId

### Project Roles
- GET /api/v1/projects/:projectId/roles
- POST /api/v1/projects/:projectId/roles
- PATCH /api/v1/projects/:projectId/roles/:roleId
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

### GitHub Integration
- GET /api/v1/projects/:projectId/github
- PUT /api/v1/projects/:projectId/github/token
- DELETE /api/v1/projects/:projectId/github/token
- GET /api/v1/projects/:github/repositories
- GET /api/v1/projects/:github/linked-repositories
- POST /api/v1/projects/:github/linked-repositories
- DELETE /api/v1/projects/:github/linked-repositories/:repoId

### Task Activities
- GET /api/v1/projects/:projectId/tasks/:taskId/activities
- POST /api/v1/projects/:projectId/tasks/:taskId/activities/comments
- PATCH /api/v1/projects/:projectId/tasks/:taskId/activities/comments/:commentId
- DELETE /api/v1/projects/:projectId/tasks/:taskId/activities/comments/:commentId

### Task GitHub
- GET /api/v1/projects/:projectId/tasks/:taskId/github/pull-requests
- POST /api/v1/projects/:projectId/tasks/:taskId/github/pull-requests
- DELETE /api/v1/projects/:projectId/tasks/:taskId/github/pull-requests/:prId
- POST /api/v1/projects/:projectId/tasks/:taskId/github/branches
- GET /api/v1/projects/:projectId/tasks/:taskId/github/branches

## File Structure

```
src/
├── api/
│   ├── client.ts              # Base API client
│   ├── extended-client.ts     # Members, roles, task types, statuses
│   ├── views-client.ts        # Views, custom fields, attachments
│   ├── task-extended-client.ts # Activities, comments, task GitHub
│   ├── doc-client.ts          # Doc folders, snapshots, files
│   ├── github-client.ts       # GitHub integration
│   └── index.ts               # Exports
├── tools/
│   ├── project-tools.ts       # Project tools
│   ├── task-tools.ts          # Task tools
│   ├── sprint-tools.ts        # Sprint tools
│   ├── document-tools.ts      # Document tools
│   ├── member-tools.ts        # Member & role tools
│   ├── task-type-tools.ts     # Task type & status tools
│   ├── view-tools.ts          # View & custom field tools
│   ├── attachment-tools.ts    # Attachment tools
│   ├── doc-github-tools.ts    # Doc & GitHub tools
│   ├── task-activity-tools.ts # Task activity & GitHub tools
│   └── index.ts               # Tool registry & router
├── types/
│   └── index.ts               # All type definitions
├── utils/
│   ├── converters.ts          # BlockNote ↔ Markdown
│   ├── formatters.ts          # Output formatting
│   └── index.ts               # Exports
├── server.ts                  # MCP server setup
└── index.ts                   # Entry point
```

## Usage Examples

### Creating a Complete Workflow

```typescript
// 1. Create a project
create_project({
  name: "New Project",
  description: "Project description"
})

// 2. Add a member
add_project_member({
  projectId: "project-id",
  userId: "user-id",
  roleId: "role-id"
})

// 3. Create a task type
create_task_type({
  projectId: "project-id",
  name: "Bug",
  color: "#ff0000"
})

// 4. Create a task
create_task({
  projectId: "project-id",
  title: "Fix critical bug",
  description: "# Bug Details\n\nSteps to reproduce...",
  typeId: "task-type-id"
})

// 5. Add a comment
add_task_comment({
  projectId: "project-id",
  taskId: "task-id",
  text: "Ready for review."
})
```

### GitHub Integration

```typescript
// 1. Set GitHub token
set_github_token({
  projectId: "project-id",
  token: "ghp_xxxxxxxxxxxx"
})

// 2. List repositories
list_github_repositories({ projectId: "project-id" })

// 3. Link a repository
link_github_repository({
  projectId: "project-id",
  owner: "username",
  repo: "repository-name"
})

// 4. Create a branch for a task
create_branch_for_task({
  projectId: "project-id",
  taskId: "task-id",
  branchName: "fix/bug-123"
})
```

## Notes

- All tools support API key authentication
- Markdown descriptions are automatically converted to BlockNote format
- BlockNote content is automatically converted to Markdown when reading
- All operations require appropriate project permissions
- File uploads require a two-step process: initiate upload → complete upload
