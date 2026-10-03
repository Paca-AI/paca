package taskdom

// TaskField names a user-editable task attribute, so a partial update can say
// exactly which columns it writes.
type TaskField string

// Task fields a partial update can write. The values match the task columns,
// except TaskFieldAssignees, which lives in the task_assignees table.
const (
	TaskFieldTaskType       TaskField = "task_type_id"
	TaskFieldStatus         TaskField = "status_id"
	TaskFieldSprint         TaskField = "sprint_id"
	TaskFieldParentTask     TaskField = "parent_task_id"
	TaskFieldTitle          TaskField = "title"
	TaskFieldDescription    TaskField = "description"
	TaskFieldImportance     TaskField = "importance"
	TaskFieldStoryPoints    TaskField = "story_points"
	TaskFieldAssignees      TaskField = "assignees"
	TaskFieldReporter       TaskField = "reporter_id"
	TaskFieldCustomFields   TaskField = "custom_fields"
	TaskFieldStartDate      TaskField = "start_date"
	TaskFieldDueDate        TaskField = "due_date"
	TaskFieldTags           TaskField = "tags"
	TaskFieldAssignmentMode TaskField = "assignment_mode"
)
