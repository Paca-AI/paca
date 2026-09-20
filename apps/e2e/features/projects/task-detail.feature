@projects @interactions @task-detail
Feature: Task detail
  The task detail view is the primary interface for viewing and editing a
  single task.  It opens as a modal dialog when a task card (Board view)
  or task row (Table view) is clicked, and as a standalone page when
  navigated to via a direct URL.  Both renderings share the same two-pane
  layout: a scrollable content pane on the left (title, a unified
  properties section, description, subtasks, checklists, and attachments)
  and an activity pane on the right (event log and comment thread).
  The unified properties section renders all built-in fields and every
  project-defined custom field together in a single two-column grid —
  custom fields do not appear in a separate section.

  @authenticated
  Rule: Opening — modal from a board or table view

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_MODAL_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Board" view and a "Table" view
      And the interaction has a task named "E2E_MODAL_TASK"

    Scenario: Clicking a task card in the Board view opens the task detail as a modal
      Given the user has navigated to the "Product Backlog" board view inside "E2E_MODAL_PROJECT"
      When the user clicks the task card for "E2E_MODAL_TASK"
      Then the task detail modal should open
      And the modal should display the title "E2E_MODAL_TASK"
      And the board view should still be visible in the background

    Scenario: Clicking a task row in the Table view opens the task detail as a modal
      Given the user has navigated to the "Product Backlog" table view inside "E2E_MODAL_PROJECT"
      When the user clicks the row for task "E2E_MODAL_TASK"
      Then the task detail modal should open
      And the modal should display the title "E2E_MODAL_TASK"

    Scenario: The URL updates to include the task identifier when the modal opens
      Given the user has navigated to the "Product Backlog" board view inside "E2E_MODAL_PROJECT"
      When the user clicks the task card for "E2E_MODAL_TASK"
      Then the page URL should include the task identifier for "E2E_MODAL_TASK"

  @authenticated
  Rule: Opening — task detail page from a direct URL

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_PAGE_PROJECT" exists
      And the project has a "Product Backlog" interaction
      And the interaction has a task named "E2E_PAGE_TASK"

    Scenario: Navigating directly to a task URL renders the full-page layout
      When the user navigates directly to the task detail URL for "E2E_PAGE_TASK"
      Then the task detail page should be displayed
      And the page should not be overlaid on any view
      And the page should display the title "E2E_PAGE_TASK"

    Scenario: The task detail page shows a breadcrumb with project and interaction context
      When the user navigates directly to the task detail URL for "E2E_PAGE_TASK"
      Then a breadcrumb should be visible in the page header
      And the breadcrumb should include the project name "E2E_PAGE_PROJECT"
      And the breadcrumb should include the interaction name

    Scenario: Navigating to a non-existent task URL shows a not-found state
      When the user navigates directly to a task detail URL for a task that does not exist
      Then a "Task not found" message should be displayed

  @authenticated
  Rule: Two-pane layout

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_LAYOUT_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Board" view
      And the interaction has a task named "E2E_LAYOUT_TASK"
      And the user has navigated to the "Product Backlog" board view inside "E2E_LAYOUT_PROJECT"

    Scenario: The content pane and activity pane are displayed side by side
      When the user opens the task detail for "E2E_LAYOUT_TASK"
      Then the task detail should show a content pane on the left
      And the task detail should show an activity pane on the right

    Scenario: The content pane contains the title, properties, and body sections
      When the user opens the task detail for "E2E_LAYOUT_TASK"
      Then the content pane should contain the task title
      And the content pane should contain the properties section
      And the content pane should contain the description area
      And the content pane should contain the subtasks section
      And the content pane should contain the checklists section
      And the content pane should contain the attachments section

    Scenario: The activity pane contains the event log and comment input
      When the user opens the task detail for "E2E_LAYOUT_TASK"
      Then the activity pane should contain an activity log showing task events
      And the activity pane should contain a rich-text comment editor

    Scenario: The header shows the task short ID, created date, and action controls
      When the user opens the task detail for "E2E_LAYOUT_TASK"
      Then the task detail header should display the task short ID
      And the task detail header should display the created date
      And the task detail header should contain a "Share" button

  @authenticated
  Rule: Header — task type badge, status chip, and title

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_HEADER_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Board" view
      And the user has navigated to the "Product Backlog" board view inside "E2E_HEADER_PROJECT"

    Scenario: Task type badge is shown when a task has a type assigned
      Given the interaction has a task "E2E_TYPED_TASK" with the task type "Bug"
      When the user opens the task detail for "E2E_TYPED_TASK"
      Then the task detail should display a type badge labelled "Bug"
      And the badge should be styled with the colour associated with the "Bug" type

    Scenario: Task type badge is hidden when a task has no type assigned
      Given the interaction has a task "E2E_UNTYPED_TASK" with no task type
      When the user opens the task detail for "E2E_UNTYPED_TASK"
      Then the task detail should not display a type badge

    Scenario: Status chip is displayed in the header when a task has a status
      Given the interaction has a task "E2E_STATUS_TASK" with status "In Progress"
      When the user opens the task detail for "E2E_STATUS_TASK"
      Then the task detail header should display a status chip labelled "In Progress"

    Scenario: Task title is always present and prominently displayed
      Given the interaction has a task "E2E_TITLE_TASK"
      When the user opens the task detail for "E2E_TITLE_TASK"
      Then the task detail should display the title "E2E_TITLE_TASK"

  @authenticated
  Rule: Properties section — unified field grid

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_PROPS_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Board" view
      And the interaction has a task named "E2E_PROPS_TASK"
      And the user has navigated to the "Product Backlog" board view inside "E2E_PROPS_PROJECT"

    Scenario: The properties section is rendered as a two-column label-value grid
      When the user opens the task detail for "E2E_PROPS_TASK"
      Then the properties section should render fields in a two-column grid layout

    Scenario: Status field is always present in the properties grid
      When the user opens the task detail for "E2E_PROPS_TASK"
      Then the properties section should contain a "Status" field

    Scenario: Dates field is always present with Start and Due date controls
      When the user opens the task detail for "E2E_PROPS_TASK"
      Then the properties section should contain a "Dates" field
      And the "Dates" field should display a "Start" date input and a "Due" date input

    Scenario: Start and due date values are shown when set
      Given the interaction has a task "E2E_DATED_TASK" with start date "2026-05-01" and due date "2026-05-31"
      When the user opens the task detail for "E2E_DATED_TASK"
      Then the "Dates" field should display "May 1, 2026" as the start date
      And the "Dates" field should display "May 31, 2026" as the due date

    Scenario: Track Time field is always present with an "Add time" action
      When the user opens the task detail for "E2E_PROPS_TASK"
      Then the properties section should contain a "Track Time" field with an "Add time" action

    Scenario: Relationships field is always present in the properties grid
      When the user opens the task detail for "E2E_PROPS_TASK"
      Then the properties section should contain a "Relationships" field

    Scenario: Assignees field is always present in the properties grid
      When the user opens the task detail for "E2E_PROPS_TASK"
      Then the properties section should contain an "Assignees" field

    Scenario: Importance field is always present in the properties grid
      When the user opens the task detail for "E2E_PROPS_TASK"
      Then the properties section should contain an "Importance" field

    Scenario: Tags field is always present in the properties grid
      When the user opens the task detail for "E2E_PROPS_TASK"
      Then the properties section should contain a "Tags" field

    Scenario: Importance level "None" (0) is displayed correctly
      Given the interaction has a task "E2E_IMPORTANCE_NONE_TASK" with importance "None"
      When the user opens the task detail for "E2E_IMPORTANCE_NONE_TASK"
      Then the "Importance" field should display the label "None"

    Scenario: Importance level "Low" (1) is displayed correctly
      Given the interaction has a task "E2E_IMPORTANCE_LOW_TASK" with importance "Low"
      When the user opens the task detail for "E2E_IMPORTANCE_LOW_TASK"
      Then the "Importance" field should display the label "Low"

    Scenario: Importance level "Medium" (2) is displayed correctly
      Given the interaction has a task "E2E_IMPORTANCE_MEDIUM_TASK" with importance "Medium"
      When the user opens the task detail for "E2E_IMPORTANCE_MEDIUM_TASK"
      Then the "Importance" field should display the label "Medium"

    Scenario: Importance level "High" (3) is displayed correctly
      Given the interaction has a task "E2E_IMPORTANCE_HIGH_TASK" with importance "High"
      When the user opens the task detail for "E2E_IMPORTANCE_HIGH_TASK"
      Then the "Importance" field should display the label "High"

    Scenario: Importance level "Critical" (4) is displayed correctly
      Given the interaction has a task "E2E_IMPORTANCE_CRITICAL_TASK" with importance "Critical"
      When the user opens the task detail for "E2E_IMPORTANCE_CRITICAL_TASK"
      Then the "Importance" field should display the label "Critical"

    Scenario: Assignees field shows the member name when an assignee is set
      Given the interaction has a task "E2E_ASSIGNED_TASK" assigned to a project member
      When the user opens the task detail for "E2E_ASSIGNED_TASK"
      Then the "Assignees" field should show the member's name or avatar

    Scenario: Assignees field shows "Empty" when no assignee is set
      Given the interaction has a task "E2E_UNASSIGNED_TASK" with no assignee
      When the user opens the task detail for "E2E_UNASSIGNED_TASK"
      Then the "Assignees" field should show an "Empty" placeholder

    Scenario: Tags field displays tag values when tags are set
      Given the interaction has a task "E2E_TAGGED_TASK" with tags "design" and "frontend"
      When the user opens the task detail for "E2E_TAGGED_TASK"
      Then the "Tags" field should display "design" and "frontend"

    Scenario: Tags field shows "Empty" when no tags are set
      Given the interaction has a task "E2E_UNTAGGED_TASK" with no tags
      When the user opens the task detail for "E2E_UNTAGGED_TASK"
      Then the "Tags" field should show an "Empty" placeholder

    Scenario: Reporter field is shown when a reporter is set on the task
      Given the interaction has a task "E2E_REPORTER_TASK" whose reporter is a project member
      When the user opens the task detail for "E2E_REPORTER_TASK"
      Then the properties section should show a "Reporter" field with the member's name

    Scenario: Sprint field is shown when the task belongs to a sprint
      Given the interaction has a task "E2E_SPRINT_TASK" in an active sprint "E2E_DETAIL_SPRINT"
      When the user opens the task detail for "E2E_SPRINT_TASK"
      Then the properties section should show a "Sprint" field with the sprint name "E2E_DETAIL_SPRINT"

    Scenario: Parent task field is shown when a task has a parent
      Given the interaction has a task "E2E_PARENT_TASK"
      And the interaction has a sub-task "E2E_CHILD_TASK" whose parent is "E2E_PARENT_TASK"
      When the user opens the task detail for "E2E_CHILD_TASK"
      Then the properties section should show a "Parent task" field with the title "E2E_PARENT_TASK"

    Scenario: Reporter, Sprint, and Parent task fields are hidden when their values are not set
      Given the interaction has a task "E2E_MINIMAL_TASK" with no reporter, no sprint, and no parent
      When the user opens the task detail for "E2E_MINIMAL_TASK"
      Then the properties section should not show a "Reporter" field
      And the properties section should not show a "Sprint" field
      And the properties section should not show a "Parent task" field

  @authenticated
  Rule: Properties section — custom fields inline with built-in fields

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_INLINE_CUSTOM_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Board" view
      And the user has navigated to the "Product Backlog" board view inside "E2E_INLINE_CUSTOM_PROJECT"

    Scenario: A custom field with a value appears inside the same properties grid as built-in fields
      Given the project has a custom field with display name "E2E Release Tag" and field key "release_tag" of type "Text"
      And the interaction has a task "E2E_CUSTOM_INLINE_TASK" with "release_tag" set to "v2.1.0" in its custom_fields
      When the user opens the task detail for "E2E_CUSTOM_INLINE_TASK"
      Then the properties section should contain an "E2E Release Tag" field with value "v2.1.0"
      And the "E2E Release Tag" field should appear within the same grid as the "Status" and "Assignees" fields

    Scenario: A custom field with no value shows an "Empty" placeholder in the shared grid
      Given the project has a custom field with display name "E2E Release Tag" and field key "release_tag" of type "Text"
      And the interaction has a task "E2E_CUSTOM_EMPTY_TASK" whose custom_fields does not contain "release_tag"
      When the user opens the task detail for "E2E_CUSTOM_EMPTY_TASK"
      Then the properties section should contain an "E2E Release Tag" field showing an "Empty" placeholder

    Scenario: Multiple custom fields each appear as separate rows in the shared grid
      Given the project has a custom field with display name "E2E Severity" and field key "severity" of type "Select"
      And the project has a custom field with display name "E2E Story Points" and field key "story_points" of type "Number"
      And the interaction has a task "E2E_MULTI_CUSTOM_TASK" with "severity" set to "High" and "story_points" set to "5"
      When the user opens the task detail for "E2E_MULTI_CUSTOM_TASK"
      Then the properties section should contain an "E2E Severity" field with value "High"
      And the properties section should contain an "E2E Story Points" field with value "5"

    Scenario: There is no separate "Custom fields" heading — all fields share one grid
      Given the project has a custom field with display name "E2E Extra Info" and field key "extra_info" of type "Text"
      And the interaction has a task "E2E_NO_SEPARATE_SECTION_TASK" with "extra_info" set to "some value"
      When the user opens the task detail for "E2E_NO_SEPARATE_SECTION_TASK"
      Then the task detail should not contain a heading labelled "Custom fields"
      And the "E2E Extra Info" field should be visible within the properties section

    Scenario: Deleting a custom field definition removes it from the properties grid
      Given the project has a custom field with display name "E2E Removed Field" and field key "removed_field" of type "Text"
      And the interaction has a task "E2E_REMOVED_FIELD_TASK" with "removed_field" set to "old value"
      When an admin deletes the "E2E Removed Field" custom field from the project settings
      And the user opens the task detail for "E2E_REMOVED_FIELD_TASK"
      Then the properties section should not contain an "E2E Removed Field" field

  @authenticated
  Rule: Properties section — "Add fields" action

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_ADD_FIELDS_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Board" view
      And the interaction has a task named "E2E_ADD_FIELDS_TASK"
      And the user has navigated to the "Product Backlog" board view inside "E2E_ADD_FIELDS_PROJECT"

    Scenario: An "Add fields" control is visible below the properties grid
      When the user opens the task detail for "E2E_ADD_FIELDS_TASK"
      Then the task detail should display an "Add fields" control below the properties grid

    Scenario: Clicking "Add fields" opens the custom field creation interface
      When the user opens the task detail for "E2E_ADD_FIELDS_TASK"
      And the user clicks the "Add fields" control
      Then the custom field creation interface should open

  @authenticated
  Rule: Description section

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_DESC_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Board" view
      And the user has navigated to the "Product Backlog" board view inside "E2E_DESC_PROJECT"

    Scenario: Description content is displayed when a task has a description
      Given the interaction has a task "E2E_WITH_DESC_TASK" with description "This is the task description."
      When the user opens the task detail for "E2E_WITH_DESC_TASK"
      Then the description area should display "This is the task description."

    Scenario: An "Add description" prompt is shown when no description is set
      Given the interaction has a task "E2E_NO_DESC_TASK" with no description
      When the user opens the task detail for "E2E_NO_DESC_TASK"
      Then the description area should show an "Add description" prompt

    Scenario: A "Write with AI" action is shown alongside the description area
      Given the interaction has a task "E2E_AI_DESC_TASK"
      When the user opens the task detail for "E2E_AI_DESC_TASK"
      Then the description area should show a "Write with AI" action

  @authenticated
  Rule: Subtasks section

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_SUBTASK_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Board" view
      And the user has navigated to the "Product Backlog" board view inside "E2E_SUBTASK_PROJECT"

    Scenario: Subtasks section is always visible with an "Add Task" button
      Given the interaction has a task "E2E_PARENT_TASK"
      When the user opens the task detail for "E2E_PARENT_TASK"
      Then the task detail should display a subtasks section labelled "Add subtask"
      And the section should contain an "Add Task" button

    Scenario: Existing subtasks are listed in the subtasks section
      Given the interaction has a task "E2E_TASK_WITH_SUBTASKS"
      And the task has a subtask named "E2E_SUBTASK_ONE"
      And the task has a subtask named "E2E_SUBTASK_TWO"
      When the user opens the task detail for "E2E_TASK_WITH_SUBTASKS"
      Then the subtasks section should display "E2E_SUBTASK_ONE"
      And the subtasks section should display "E2E_SUBTASK_TWO"

    Scenario: Subtasks section shows only the "Add Task" button when no subtasks exist
      Given the interaction has a task "E2E_NO_SUBTASK_TASK" with no subtasks
      When the user opens the task detail for "E2E_NO_SUBTASK_TASK"
      Then the subtasks section should show only the "Add Task" button and no task rows

  @authenticated
  Rule: Checklists section

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_CHECKLIST_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Board" view
      And the user has navigated to the "Product Backlog" board view inside "E2E_CHECKLIST_PROJECT"

    Scenario: Checklists section is always visible with a "Create checklist" control
      Given the interaction has a task "E2E_CHECKLIST_BASE_TASK"
      When the user opens the task detail for "E2E_CHECKLIST_BASE_TASK"
      Then the task detail should display a "Checklists" section
      And the section should contain a "Create checklist" button

  @authenticated
  Rule: Attachments section

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_ATTACH_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Board" view
      And the interaction has a task named "E2E_ATTACH_TASK"
      And the user has navigated to the "Product Backlog" board view inside "E2E_ATTACH_PROJECT"

    Scenario: Attachments section is always visible in the content pane
      When the user opens the task detail for "E2E_ATTACH_TASK"
      Then the task detail should display an "Attachments" section

    Scenario: Attachments section shows a drop-zone when no files are attached
      When the user opens the task detail for "E2E_ATTACH_TASK"
      Then the attachments section should show a "Drop your files here to upload" drop-zone

    Scenario: Existing attachments are listed with their file name
      Given the task "E2E_ATTACH_TASK" has an attachment named "design-spec.pdf"
      When the user opens the task detail for "E2E_ATTACH_TASK"
      Then the attachments section should list "design-spec.pdf"

    Scenario: Dragging a file onto the drop-zone uploads it as an attachment
      When the user opens the task detail for "E2E_ATTACH_TASK"
      And the user drags a file named "mockup.png" onto the attachments drop-zone
      Then the attachments section should list "mockup.png" after the upload completes

    Scenario: Clicking the upload icon in the attachments section opens a file picker
      When the user opens the task detail for "E2E_ATTACH_TASK"
      And the user clicks the upload icon in the attachments section
      Then a file picker dialog should open

    Scenario: An attachment can be deleted by a user with write permission
      Given the task "E2E_ATTACH_TASK" has an attachment named "old-file.docx"
      And the user has the "Edit Tasks" project permission in "E2E_ATTACH_PROJECT"
      When the user opens the task detail for "E2E_ATTACH_TASK"
      And the user clicks the delete button for attachment "old-file.docx"
      Then "old-file.docx" should be removed from the attachments section

    Scenario: A user without write permission cannot delete attachments
      Given the task "E2E_ATTACH_TASK" has an attachment named "protected-file.pdf"
      And the user does not have the "Edit Tasks" project permission in "E2E_ATTACH_PROJECT"
      When the user opens the task detail for "E2E_ATTACH_TASK"
      Then no delete button should be visible for attachment "protected-file.pdf"

    Scenario: Each attachment item shows the file size in a human-readable format
      Given the task "E2E_ATTACH_TASK" has an attachment named "report.pdf"
      When the user opens the task detail for "E2E_ATTACH_TASK"
      Then the attachment "report.pdf" should display its file size

    Scenario: Each attachment item shows a relative upload timestamp
      Given the task "E2E_ATTACH_TASK" has a recently uploaded attachment named "notes.txt"
      When the user opens the task detail for "E2E_ATTACH_TASK"
      Then the attachment "notes.txt" should display a relative upload time such as "just now"

    Scenario: The attachments section heading shows a count of attached files
      Given the task "E2E_ATTACH_TASK" has an attachment named "file-one.pdf"
      And the task "E2E_ATTACH_TASK" has an attachment named "file-two.png"
      When the user opens the task detail for "E2E_ATTACH_TASK"
      Then the "Attachments" section heading should display a count of "2"

    Scenario: Clicking the file name opens the attachment for preview in a new browser tab
      Given the task "E2E_ATTACH_TASK" has an attachment named "design.png"
      When the user opens the task detail for "E2E_ATTACH_TASK"
      And the user clicks the file name for attachment "design.png"
      Then the attachment should be opened for preview in a new browser tab

    Scenario: Clicking the file extension badge opens the attachment for preview in a new browser tab
      Given the task "E2E_ATTACH_TASK" has an attachment named "spec.pdf"
      When the user opens the task detail for "E2E_ATTACH_TASK"
      And the user clicks the file extension badge for attachment "spec.pdf"
      Then the attachment should be opened for preview in a new browser tab

    Scenario: A download button appears when hovering over an attachment row
      Given the task "E2E_ATTACH_TASK" has an attachment named "export.csv"
      When the user opens the task detail for "E2E_ATTACH_TASK"
      And the user hovers over the attachment row for "export.csv"
      Then a "Download" button should appear for "export.csv"

    Scenario: Clicking the download button initiates a file download
      Given the task "E2E_ATTACH_TASK" has an attachment named "export.csv"
      When the user opens the task detail for "E2E_ATTACH_TASK"
      And the user hovers over the attachment row for "export.csv"
      And the user clicks the "Download" button for "export.csv"
      Then a download for "export.csv" should be initiated

    Scenario: Multiple files can be selected and uploaded at once via the file picker
      When the user opens the task detail for "E2E_ATTACH_TASK"
      And the user clicks the upload icon and selects "document-a.txt" and "document-b.txt" simultaneously
      Then the attachments section should list "document-a.txt" after the upload completes
      And the attachments section should list "document-b.txt" after the upload completes

    Scenario: Multiple files can be uploaded by dropping them onto the drop zone at the same time
      When the user opens the task detail for "E2E_ATTACH_TASK"
      And the user drops "file-x.png" and "file-y.png" onto the attachments drop-zone simultaneously
      Then the attachments section should list "file-x.png" after the upload completes
      And the attachments section should list "file-y.png" after the upload completes

    Scenario: Clicking the drop zone body opens the file picker
      When the user opens the task detail for "E2E_ATTACH_TASK"
      And the user clicks directly on the drop zone body in the attachments section
      Then a file picker dialog should open

    Scenario: The upload button and drop zone are hidden for a user without write permission
      Given the user does not have the "Edit Tasks" project permission in "E2E_ATTACH_PROJECT"
      When the user opens the task detail for "E2E_ATTACH_TASK"
      Then the "Upload attachment" button should not be visible in the attachments section
      And the "Drop your files here to upload" drop zone should not be visible

    Scenario: The drop zone shows an active highlighted state while a file is dragged over it
      When the user opens the task detail for "E2E_ATTACH_TASK"
      And the user drags a file over the drop zone without releasing
      Then the drop zone should display an active highlighted visual state

    Scenario: Dropping a file when not hovering over the drop zone does not upload it
      When the user opens the task detail for "E2E_ATTACH_TASK"
      And the user drops a file onto the task detail modal outside the drop zone
      Then no new attachment should appear in the attachments section

  @authenticated
  Rule: Activity and comments panel

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_ACTIVITY_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Board" view
      And the interaction has a task named "E2E_ACTIVITY_TASK"
      And the user has navigated to the "Product Backlog" board view inside "E2E_ACTIVITY_PROJECT"

    Scenario: Activity log shows a "created this task" event for a new task
      When the user opens the task detail for "E2E_ACTIVITY_TASK"
      Then the activity pane should contain a "created this task" event entry
      And the event entry should display the creation timestamp

    Scenario: Activity log shows field-change events when properties are updated
      Given the task "E2E_ACTIVITY_TASK" had its assignee changed after creation
      When the user opens the task detail for "E2E_ACTIVITY_TASK"
      Then the activity pane should include an assignee-change event entry

    Scenario: A comment input is always visible at the bottom of the activity pane
      When the user opens the task detail for "E2E_ACTIVITY_TASK"
      Then the activity pane should display a rich-text comment editor

    Scenario: A rich-text toolbar is shown next to the comment input
      When the user opens the task detail for "E2E_ACTIVITY_TASK"
      Then the comment area should show a formatting toolbar with emoji, attachment, mention, and send controls

    Scenario: Activity count badge shows the number of activity entries
      Given the task "E2E_ACTIVITY_TASK" has at least two activity entries
      When the user opens the task detail for "E2E_ACTIVITY_TASK"
      Then the activity pane header should show a count badge reflecting the number of entries

  @authenticated
  Rule: Closing the task detail modal

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_CLOSE_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Board" view
      And the interaction has a task named "E2E_CLOSE_TASK"
      And the user has navigated to the "Product Backlog" board view inside "E2E_CLOSE_PROJECT"
      And the user has opened the task detail modal for "E2E_CLOSE_TASK"

    Scenario: Clicking the close button dismisses the modal
      When the user clicks the close button in the task detail header
      Then the task detail modal should no longer be visible
      And the board view should still be displayed with its task cards

    Scenario: Pressing Escape dismisses the modal
      When the user presses the Escape key
      Then the task detail modal should no longer be visible

    Scenario: Clicking the backdrop dismisses the modal
      When the user clicks outside the task detail modal on the backdrop
      Then the task detail modal should no longer be visible

    Scenario: After the modal closes the URL reverts to the view URL
      When the user clicks the close button in the task detail header
      Then the page URL should no longer include the task identifier

  @authenticated
  Rule: Custom field definitions — project settings management

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_CUSTOM_FIELDS_PROJECT" exists
      And the user has the "Manage Project Settings" permission in "E2E_CUSTOM_FIELDS_PROJECT"
      And the user has navigated to the "E2E_CUSTOM_FIELDS_PROJECT" Settings page

    Scenario: "Custom Fields" section is reachable from the settings sidebar
      When the user clicks "Custom Fields" in the settings sidebar
      Then the "Custom Fields" section heading should be visible
      And the section should display a description mentioning project-level custom task fields

    Scenario: "New field" button is visible on the Custom Fields section
      When the user clicks "Custom Fields" in the settings sidebar
      Then the "New field" button should be visible

    Scenario: Opening the create-field dialog
      When the user clicks "Custom Fields" in the settings sidebar
      And the user clicks the "New field" button
      Then the "Create custom field" dialog should open
      And the dialog should contain a required "Display name" field
      And the dialog should contain a required "Field key" field
      And the dialog should contain a "Field type" dropdown
      And the dialog should contain a "Required" toggle

    Scenario: Field key is auto-derived from the display name
      When the user clicks "Custom Fields" in the settings sidebar
      And the user clicks the "New field" button
      And the user fills the display name with "Release Tag"
      Then the "Field key" field should be auto-populated with a slugified value such as "release_tag"

    Scenario: Field type dropdown lists the available types
      When the user clicks "Custom Fields" in the settings sidebar
      And the user clicks the "New field" button
      And the user opens the "Field type" dropdown
      Then the dropdown should list "Text"
      And the dropdown should list "Number"
      And the dropdown should list "Date"
      And the dropdown should list "Checkbox"
      And the dropdown should list "Select"

    Scenario: Select field type reveals an options editor
      When the user clicks "Custom Fields" in the settings sidebar
      And the user clicks the "New field" button
      And the user selects "Select" from the "Field type" dropdown
      Then an "Options" editor should appear allowing the user to add selectable values

    Scenario: "Create field" button is disabled while the display name is empty
      When the user clicks "Custom Fields" in the settings sidebar
      And the user clicks the "New field" button
      Then the "Create field" button should be disabled

    Scenario: "Create field" button becomes enabled after typing a display name
      When the user clicks "Custom Fields" in the settings sidebar
      And the user clicks the "New field" button
      And the user fills the display name with "E2E Release Tag"
      Then the "Create field" button should be enabled

    Scenario: Creating a non-required Text custom field succeeds
      When the user clicks "Custom Fields" in the settings sidebar
      And the user clicks the "New field" button
      And the user fills the display name with "E2E Release Tag"
      And the user selects "Text" from the "Field type" dropdown
      And the user clicks "Create field"
      Then the dialog should close
      And the custom fields table should contain a field with display name "E2E Release Tag" and type "Text"

    Scenario: Creating a required custom field marks it as required in the table
      When the user clicks "Custom Fields" in the settings sidebar
      And the user clicks the "New field" button
      And the user fills the display name with "E2E Required Field"
      And the user selects "Text" from the "Field type" dropdown
      And the user enables the "Required" toggle
      And the user clicks "Create field"
      Then the custom fields table should show "E2E Required Field" as required

    Scenario: Creating a Select field with options stores the options list
      When the user clicks "Custom Fields" in the settings sidebar
      And the user clicks the "New field" button
      And the user fills the display name with "E2E Severity"
      And the user selects "Select" from the "Field type" dropdown
      And the user adds the options "Low", "Medium", and "High"
      And the user clicks "Create field"
      Then the custom fields table should contain a field with display name "E2E Severity" and type "Select"

    Scenario: Editing an existing custom field updates its display name
      Given a custom field with display name "E2E Old Display Name" and field key "old_display_name" exists in the project
      When the user clicks "Custom Fields" in the settings sidebar
      And the user clicks the "Edit field" button for "E2E Old Display Name"
      And the user clears the display name and types "E2E New Display Name"
      And the user clicks "Save changes"
      Then the custom fields table should contain a field with display name "E2E New Display Name"
      And the custom fields table should not contain a field with display name "E2E Old Display Name"

    Scenario: Deleting a custom field removes it from the table
      Given a custom field with display name "E2E Delete Me Field" and field key "delete_me_field" exists in the project
      When the user clicks "Custom Fields" in the settings sidebar
      And the user clicks the "Delete field" button for "E2E Delete Me Field"
      And the user confirms the deletion
      Then the custom fields table should not contain a field with display name "E2E Delete Me Field"

    Scenario: User without "Manage Project Settings" permission cannot see the "New field" button
      Given the user does not have the "Manage Project Settings" permission in "E2E_CUSTOM_FIELDS_PROJECT"
      When the user clicks "Custom Fields" in the settings sidebar
      Then the "New field" button should not be visible
