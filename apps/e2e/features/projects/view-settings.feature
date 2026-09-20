@projects @interactions @views @settings
Feature: View settings — dynamic fields, grouping, sorting, and aggregation
  The view settings panel lets a user customise how tasks are displayed in
  Board and Table views.  Every display setting (Fields, Column by, Swimlanes,
  Sort by, Field sum, Initial size, Per page) and every filter (Sprints,
  Statuses, Assignees, Task types, Start Date, Due Date, Importance, Story
  Points, Tags, plus one per custom field) works for ALL task fields — built-in fields (Status,
  Assignee, Importance, Task Type, Reporter, Start Date, Due Date, Title,
  Created) AND custom fields (text, number, date, select, multi_select,
  boolean, url).  The term "Importance" is used instead of "Priority" in all
  labels.  When grouping by a field ("Column by"), ALL possible column values
  are rendered even when they contain zero tasks.

  Settings are per view AND per user.  A view itself is shared by the whole
  project, but a member's edits live in a personal override (a
  "user_view_configs" row) that other members never see.  What the footer
  offers depends on the "views.write" project permission:
    - A views.write holder (Admin/Editor by default) sees a split button:
      "Save for everyone" publishes the draft as the team default (and clears
      that user's own override); the dropdown next to it offers "Save only
      for me" for a personal override.
    - Anyone else (e.g. Viewer) sees a single "Save" button, which is always
      personal-only — they can tweak how THEY see a view without being able
      to change the shared view.
  After a personal save the panel header shows an "Only visible to you"
  badge.  "Reset" (shown only when the draft differs from the team default)
  reverts the draft to the shared/team config — not to the user's last
  personal save — and, like any edit, still needs a save to take effect.
  Save buttons stay disabled until the draft actually differs from what they
  would replace.

  Scenarios below that say "the user saves the view settings" mean "Save for
  everyone" for the views.write Background user; the personal/shared split
  itself is specified in the "Personal vs shared view settings" rules.

  ═══════════════════════════════════════════════════════════════════════════
  Background
  ═══════════════════════════════════════════════════════════════════════════

  @authenticated
  Background:
    Given the user already has a stored authenticated session
    And a project named "E2E_VS_PROJECT" exists
    And the project has a "Product Backlog" interaction with at least one view
    And the user has the "views.write" project permission in "E2E_VS_PROJECT"
    And the user has navigated to the "E2E_VS_PROJECT" project

  ═══════════════════════════════════════════════════════════════════════════
  Rule: View settings panel includes all built-in and custom field options
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: The view settings panel opens with all display setting rows and the Filters section
    When the user clicks the "View settings" button in the view toolbar
    Then a settings panel titled "View settings" should appear
    And the panel should display a "Fields" row
    And the panel should display a "Column by" row
    And the panel should display a "Swimlanes" row
    And the panel should display a "Sort by" row
    And the panel should display a "Field sum" row
    And the panel should display an "Initial size" row
    And the panel should display a "Per page" row
    And the panel should display a "Filters" section
    And the panel should contain a "Save for everyone" button

  Scenario: The Reset button only appears once the draft differs from the team default
    When the user clicks the "View settings" button in the view toolbar
    Then the panel should not contain a "Reset" button
    When the user selects "Title" in the "Sort by" dropdown
    Then the panel should contain a "Reset" button

  Scenario: "Column by" dropdown includes all built-in fields
    Given the project has no custom fields
    When the user clicks the "View settings" button in the view toolbar
    Then the "Column by" dropdown should include the following options:
      | Status     |
      | Sprint     |
      | Assignee   |
      | Importance |
      | Type       |
      | Reporter   |

  Scenario: "Column by" dropdown includes project custom fields of selectable types
    Given the project has a custom field "Severity" of type "select" with options "Low,Medium,High,Critical"
    And the project has a custom field "Story Points" of type "number"
    And the project has a custom field "Is Blocked" of type "boolean"
    When the user clicks the "View settings" button in the view toolbar
    Then the "Column by" dropdown should include the option "Severity"
    And the "Column by" dropdown should include the option "Story Points"
    And the "Column by" dropdown should include the option "Is Blocked"

  Scenario: "Sort by" dropdown includes all built-in sort options
    When the user clicks the "View settings" button in the view toolbar
    Then the "Sort by" dropdown should include the following options:
      | Manual       |
      | Importance   |
      | Story Points |
      | Title        |
      | Created      |
      | Start Date   |
      | Due Date     |

  Scenario: "Sort by" dropdown includes custom fields that are sortable
    Given the project has a custom field "Story Points" of type "number"
    And the project has a custom field "Target Date" of type "date"
    And the project has a custom field "Severity" of type "select" with options "Low,Medium,High"
    When the user clicks the "View settings" button in the view toolbar
    Then the "Sort by" dropdown should include the option "Story Points"
    And the "Sort by" dropdown should include the option "Target Date"
    And the "Sort by" dropdown should include the option "Severity"

  Scenario: "Swimlanes" dropdown includes None plus all fields
    Given the project has a custom field "Priority" of type "select" with options "P1,P2,P3"
    When the user clicks the "View settings" button in the view toolbar
    Then the "Swimlanes" dropdown should include the option "None"
    And the "Swimlanes" dropdown should include the option "Assignee"
    And the "Swimlanes" dropdown should include the option "Importance"
    And the "Swimlanes" dropdown should include the option "Type"
    And the "Swimlanes" dropdown should include the option "Priority"

  Scenario: "Field sum" dropdown includes Count and all numeric fields
    Given the project has a custom field "Story Points" of type "number"
    And the project has a custom field "Time Estimate" of type "number"
    When the user clicks the "View settings" button in the view toolbar
    Then the "Field sum" dropdown should include the option "Count"
    And the "Field sum" dropdown should include the option "Story Points"
    And the "Field sum" dropdown should include the option "Time Estimate"

  Scenario: The settings panel uses "Importance" label instead of "Priority"
    When the user clicks the "View settings" button in the view toolbar
    Then no dropdown in the settings panel should contain the option label "Priority"
    And all dropdowns in the settings panel that offer importance-based grouping should use the label "Importance"

  ═══════════════════════════════════════════════════════════════════════════
  Rule: "Column by" groups tasks into columns by any field
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: Column by "Status" groups tasks by their status (default)
    Given the interaction has a task "E2E_TASK_TODO" with status "Todo"
    And the interaction has a task "E2E_TASK_IP" with status "In Progress"
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Status" in the "Column by" dropdown
    And the user saves the view settings
    Then the view should display columns for each configured task status
    And the "Todo" column should contain the task "E2E_TASK_TODO"
    And the "In Progress" column should contain the task "E2E_TASK_IP"

  Scenario: Column by "Assignee" groups tasks by their assignee
    Given the interaction has a task "E2E_ALICE_TASK" assigned to "E2E_ALICE"
    And the interaction has a task "E2E_BOB_TASK" assigned to "E2E_BOB"
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Assignee" in the "Column by" dropdown
    And the user saves the view settings
    Then the view should display one column per assignee
    And a column for "E2E_ALICE" should contain the task "E2E_ALICE_TASK"
    And a column for "E2E_BOB" should contain the task "E2E_BOB_TASK"

  Scenario: Column by "Importance" groups tasks by their importance level
    Given the interaction has a task "E2E_LOW_TASK" with importance 1
    And the interaction has a task "E2E_HIGH_TASK" with importance 3
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Importance" in the "Column by" dropdown
    And the user saves the view settings
    Then the view should display one column per importance level (None, Low, Medium, High, Critical)
    And the "Low" column should contain the task "E2E_LOW_TASK"
    And the "High" column should contain the task "E2E_HIGH_TASK"

  Scenario: Column by "Type" groups tasks by their task type
    Given the interaction has a task "E2E_STORY_TASK" with type "Story"
    And the interaction has a task "E2E_BUG_TASK" with type "Bug"
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Type" in the "Column by" dropdown
    And the user saves the view settings
    Then the view should display one column per task type
    And the "Story" column should contain the task "E2E_STORY_TASK"
    And the "Bug" column should contain the task "E2E_BUG_TASK"

  Scenario: Column by a custom select field groups tasks by the field's options
    Given the project has a custom field "Severity" of type "select" with options "Low,Medium,High,Critical"
    And the interaction has a task "E2E_SEV_HIGH" with custom field "Severity" set to "High"
    And the interaction has a task "E2E_SEV_LOW" with custom field "Severity" set to "Low"
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Severity" in the "Column by" dropdown
    And the user saves the view settings
    Then the view should display one column per "Severity" option (Low, Medium, High, Critical)
    And the "High" column should contain the task "E2E_SEV_HIGH"
    And the "Low" column should contain the task "E2E_SEV_LOW"

  Scenario: Column by a custom boolean field groups tasks into True and False columns
    Given the project has a custom field "Is Blocked" of type "boolean"
    And the interaction has a task "E2E_BLOCKED_TASK" with custom field "Is Blocked" set to true
    And the interaction has a task "E2E_FREE_TASK" with custom field "Is Blocked" set to false
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Is Blocked" in the "Column by" dropdown
    And the user saves the view settings
    Then the view should display a column labelled "Yes" or "True"
    And the view should display a column labelled "No" or "False"
    And the "Yes" (or "True") column should contain the task "E2E_BLOCKED_TASK"
    And the "No" (or "False") column should contain the task "E2E_FREE_TASK"

  Scenario: Column by a custom number field groups tasks by their numeric value
    Given the project has a custom field "Story Points" of type "number"
    And the interaction has a task "E2E_SP_1" with custom field "Story Points" set to 1
    And the interaction has a task "E2E_SP_5" with custom field "Story Points" set to 5
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Story Points" in the "Column by" dropdown
    And the user saves the view settings
    Then the view should display a column for value "1"
    And the view should display a column for value "5"

  Scenario: Column by a custom multi-select field groups tasks by each selected option
    Given the project has a custom field "Labels" of type "multi_select" with options "frontend,backend,devops"
    And the interaction has a task "E2E_MULTI_TASK" with custom field "Labels" set to ["frontend", "backend"]
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Labels" in the "Column by" dropdown
    And the user saves the view settings
    Then the task "E2E_MULTI_TASK" should appear in the "frontend" column
    And the task "E2E_MULTI_TASK" should appear in the "backend" column

  ═══════════════════════════════════════════════════════════════════════════
  Rule: "Column by" keeps all columns even when empty
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: Column by Status shows all statuses even when they have no tasks
    Given the project has task statuses "Todo", "In Progress", "Done", "Review"
    And the interaction has only tasks with status "Todo"
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Status" in the "Column by" dropdown
    And the user saves the view settings
    Then the view should display columns for "Todo", "In Progress", "Done", and "Review"
    And the "In Progress" column should be visible with an empty state message
    And the "Done" column should be visible with an empty state message
    And the "Review" column should be visible with an empty state message

  Scenario: Column by a custom select field shows all options even when empty
    Given the project has a custom field "Severity" of type "select" with options "Low,Medium,High,Critical"
    And the interaction has a task "E2E_ONLY_LOW" with custom field "Severity" set to "Low"
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Severity" in the "Column by" dropdown
    And the user saves the view settings
    Then the view should display columns for "Low", "Medium", "High", and "Critical"
    And the "Medium" column should be visible with an empty state message
    And the "High" column should be visible with an empty state message
    And the "Critical" column should be visible with an empty state message

  Scenario: Column by Importance shows all importance levels even when some have no tasks
    Given the interaction has a task "E2E_IMP_LOW" with importance 1
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Importance" in the "Column by" dropdown
    And the user saves the view settings
    Then the view should display columns for "None", "Low", "Medium", "High", and "Critical"
    And the "None" column should be visible even though it has no tasks
    And the "Medium" column should be visible even though it has no tasks

  Scenario: Column by Type shows all task types even when some have no tasks
    Given the project has task types "Story", "Bug", "Task", "Epic"
    And the interaction has only tasks of type "Story"
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Type" in the "Column by" dropdown
    And the user saves the view settings
    Then the view should display columns for "Story", "Bug", "Task", and "Epic"
    And the "Bug" column should be visible with an empty state message

  Scenario: Column by Assignee shows an "Unassigned" column for tasks without assignees
    Given the interaction has an unassigned task "E2E_NO_ASSIGNEE"
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Assignee" in the "Column by" dropdown
    And the user saves the view settings
    Then the view should display a column for "Unassigned" or "No Assignee"
    And the "Unassigned" (or "No Assignee") column should contain the task "E2E_NO_ASSIGNEE"

  Scenario: Column by a custom boolean field shows both True and False columns even when one is empty
    Given the project has a custom field "Is Blocked" of type "boolean"
    And the interaction has a task "E2E_NOT_BLOCKED" with custom field "Is Blocked" set to false
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Is Blocked" in the "Column by" dropdown
    And the user saves the view settings
    Then the view should display both the "Yes" (or "True") and "No" (or "False") columns
    And the "Yes" (or "True") column should be visible with an empty state message

  ═══════════════════════════════════════════════════════════════════════════
  Rule: "Sort by" sorts tasks within each group by any field
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: Sort by "Importance" orders tasks by importance descending
    Given the interaction has a task "E2E_LOW_SORT" with importance 1
    And the interaction has a task "E2E_HIGH_SORT" with importance 3
    And the interaction has a task "E2E_MED_SORT" with importance 2
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Importance" in the "Sort by" dropdown
    And the user saves the view settings
    Then tasks within each group should be ordered by importance (highest first)
    And "E2E_HIGH_SORT" should appear before "E2E_MED_SORT"
    And "E2E_MED_SORT" should appear before "E2E_LOW_SORT"

  Scenario: Sort by "Title" orders tasks alphabetically by title
    Given the interaction has a task "Zebra Task"
    And the interaction has a task "Alpha Task"
    And the interaction has a task "Middle Task"
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Title" in the "Sort by" dropdown
    And the user saves the view settings
    Then tasks within each group should be ordered alphabetically by title
    And "Alpha Task" should appear before "Middle Task"
    And "Middle Task" should appear before "Zebra Task"

  Scenario: Sort by "Created" orders tasks by creation date
    Given the interaction has a task "E2E_OLD_TASK" created on "2025-01-01"
    And the interaction has a task "E2E_NEW_TASK" created on "2025-06-15"
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Created" in the "Sort by" dropdown
    And the user saves the view settings
    Then tasks should be ordered by creation date (oldest or newest first as configured)

  Scenario: Sort by a custom number field orders tasks numerically
    Given the project has a custom field "Story Points" of type "number"
    And the interaction has a task "E2E_SP_LOW" with custom field "Story Points" set to 1
    And the interaction has a task "E2E_SP_HIGH" with custom field "Story Points" set to 13
    And the interaction has a task "E2E_SP_MID" with custom field "Story Points" set to 5
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Story Points" in the "Sort by" dropdown
    And the user saves the view settings
    Then tasks should be ordered by their "Story Points" value

  Scenario: Sort by a custom select field orders tasks by the option order
    Given the project has a custom field "Severity" of type "select" with options "Low,Medium,High,Critical"
    And the interaction has a task "E2E_SEV_HIGH" with custom field "Severity" set to "High"
    And the interaction has a task "E2E_SEV_LOW" with custom field "Severity" set to "Low"
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Severity" in the "Sort by" dropdown
    And the user saves the view settings
    Then tasks should be ordered by the "Severity" field option order

  Scenario: Sort by "Manual" enables drag-to-reorder and disables automatic sorting
    Given the interaction has tasks "E2E_TASK_A", "E2E_TASK_B", "E2E_TASK_C" in the "Todo" group
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Manual" in the "Sort by" dropdown
    And the user saves the view settings
    Then tasks should become draggable for manual reordering
    And the task order should reflect the manually saved order

  Scenario: Changing sort from "Manual" to "Importance" disables drag handles
    Given the view is configured with "Sort by: Manual"
    And the interaction has tasks in the "Todo" group
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Importance" in the "Sort by" dropdown
    And the user saves the view settings
    Then tasks should not be draggable
    And tasks should be ordered by importance

  ═══════════════════════════════════════════════════════════════════════════
  Rule: "Swimlanes" groups tasks into horizontal bands by any field
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: Swimlanes set to "None" shows no horizontal grouping
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "None" in the "Swimlanes" dropdown
    And the user saves the view settings
    Then the view should not display horizontal swimlane bands
    And all tasks should be shown in a single flat list per column

  Scenario: Swimlanes by "Assignee" creates horizontal bands per assignee
    Given the interaction has a task "E2E_ALICE_S" assigned to "E2E_ALICE" with status "Todo"
    And the interaction has a task "E2E_BOB_S" assigned to "E2E_BOB" with status "Todo"
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Assignee" in the "Swimlanes" dropdown
    And the user saves the view settings
    Then the view should display horizontal swimlane bands labelled by assignee
    And a swimlane band for "E2E_ALICE" should contain the task "E2E_ALICE_S"
    And a swimlane band for "E2E_BOB" should contain the task "E2E_BOB_S"

  Scenario: Swimlanes by "Importance" creates horizontal bands per importance level
    Given the interaction has a task "E2E_IMP_SW_LOW" with importance 1
    And the interaction has a task "E2E_IMP_SW_HIGH" with importance 3
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Importance" in the "Swimlanes" dropdown
    And the user saves the view settings
    Then the view should display horizontal swimlane bands for each importance level
    And a swimlane band for "Low" should contain "E2E_IMP_SW_LOW"
    And a swimlane band for "High" should contain "E2E_IMP_SW_HIGH"

  Scenario: Swimlanes by a custom select field creates horizontal bands per option
    Given the project has a custom field "Component" of type "select" with options "Frontend,Backend,API"
    And the interaction has a task "E2E_FE_TASK" with custom field "Component" set to "Frontend"
    And the interaction has a task "E2E_BE_TASK" with custom field "Component" set to "Backend"
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Component" in the "Swimlanes" dropdown
    And the user saves the view settings
    Then the view should display swimlane bands for "Frontend" and "Backend"
    And the "Frontend" swimlane band should contain "E2E_FE_TASK"
    And the "Backend" swimlane band should contain "E2E_BE_TASK"

  Scenario: Swimlanes and Column by can be combined on different fields
    Given the interaction has a task "E2E_COMBO" assigned to "E2E_ALICE" with status "Todo"
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Status" in the "Column by" dropdown
    And the user selects "Assignee" in the "Swimlanes" dropdown
    And the user saves the view settings
    Then the view should display columns grouped by "Status"
    And within each column tasks should be further grouped into horizontal swimlane bands by "Assignee"

  ═══════════════════════════════════════════════════════════════════════════
  Rule: "Field sum" shows aggregation per group for any numeric field
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: Field sum defaults to "Count" showing task totals per group
    Given the interaction has 3 tasks with status "Todo"
    And the interaction has 1 task with status "In Progress"
    When the user clicks the "View settings" button in the view toolbar
    Then the "Field sum" dropdown should default to "Count"
    And the "Todo" group heading should show a count of "3"
    And the "In Progress" group heading should show a count of "1"

  Scenario: Field sum by a custom numeric field shows the total per group
    Given the project has a custom field "Story Points" of type "number"
    And the interaction has a task "E2E_SP_2" with status "Todo" and custom field "Story Points" set to 2
    And the interaction has a task "E2E_SP_3" with status "Todo" and custom field "Story Points" set to 3
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Story Points" in the "Field sum" dropdown
    And the user saves the view settings
    Then the "Todo" group heading should display a total of "5" story points

  Scenario: Field sum with no matching numeric values shows 0 for empty groups
    Given the project has a custom field "Story Points" of type "number"
    And the interaction has a task "E2E_SP_TASK" with status "Todo" and custom field "Story Points" set to 3
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Story Points" in the "Field sum" dropdown
    And the user saves the view settings
    Then the "Todo" group heading should display "3"
    And any other group headings with no "Story Points" values should display "0"

  ═══════════════════════════════════════════════════════════════════════════
  Rule: "Fields" controls which columns are visible in the table/list view
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: The "Fields" row shows the current visible field names
    When the user clicks the "View settings" button in the view toolbar
    Then the "Fields" row should display the currently visible field names

  Scenario: Clicking the "Fields" row opens a field picker with all available fields
    Given the project has a custom field "Severity" of type "select" with options "Low,High"
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks on the "Fields" row
    Then a field picker should appear listing all available fields
    And the field picker should include built-in fields: "Title", "Assignee", "Status", "Importance", "Type"
    And the field picker should include the custom field "Severity"

  Scenario: Toggling a field off hides that column from the table view
    Given the view is in Table layout
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks on the "Fields" row
    And the user unchecks the field "Importance"
    And the user saves the view settings
    Then the table view should not display an "Importance" column header
    And the table view should not display importance values in task rows

  Scenario: Toggling a custom field on shows that column in the table view
    Given the project has a custom field "Severity" of type "select" with options "Low,High"
    And the view is in Table layout
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks on the "Fields" row
    And the user checks the field "Severity"
    And the user saves the view settings
    Then the table view should display a "Severity" column header
    And each task row should display its "Severity" custom field value

  Scenario: Dragging a field in the field picker reorders the columns
    Given the view is in Table layout
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks on the "Fields" row
    And the user drags the "Importance" field above the "Title" field
    And the user saves the view settings
    Then the "Importance" column should appear before the "Title" column in the table view

  ═══════════════════════════════════════════════════════════════════════════
  Rule: The Filters section narrows which tasks a view shows
  ═══════════════════════════════════════════════════════════════════════════

  # The default Product Backlog view filters to normal task types, so the
  # "Task types" group carries a count badge of 1 from the start.
  Scenario: The Filters section lists a collapsible group per filterable field
    When the user clicks the "View settings" button in the view toolbar
    Then the "Filters" section should contain the following collapsible groups
      | Sprints      |
      | Statuses     |
      | Assignees    |
      | Task types   |
      | Start Date   |
      | Due Date     |
      | Importance   |
      | Story Points |
      | Tags         |

  Scenario: Every custom field gets its own filter group
    Given the project has a custom field "Component" of type "select" with options "Frontend,Backend,API"
    When the user clicks the "View settings" button in the view toolbar
    Then the "Filters" section should contain a "Component" group

  Scenario: Filtering by assignee only shows that assignee's tasks
    Given the interaction has a task "E2E_SLICE_ALICE" assigned to "E2E_ALICE"
    And the interaction has a task "E2E_SLICE_BOB" assigned to "E2E_BOB"
    When the user clicks the "View settings" button in the view toolbar
    And the user expands the "Assignees" filter group
    And the user selects only "E2E_ALICE" in it
    Then the view should immediately show only tasks assigned to "E2E_ALICE"
    And the "Assignees" group should display a count badge of 1
    And the task "E2E_SLICE_BOB" should not be visible

  Scenario: Filtering by a custom select field's option
    Given the project has a custom field "Component" of type "select" with options "Frontend,Backend,API"
    And the interaction has a task "E2E_FE_SLICE" with custom field "Component" set to "Frontend"
    And the interaction has a task "E2E_BE_SLICE" with custom field "Component" set to "Backend"
    When the user clicks the "View settings" button in the view toolbar
    And the user expands the "Component" filter group
    And the user selects only "Frontend" in it
    Then only tasks with "Component" set to "Frontend" should be visible

  Scenario: "Clear filters" removes every active filter
    Given the default Product Backlog view already filters to normal task types
    And the user has additionally narrowed the "Importance" group to just "Critical"
    When the user clicks the "Clear filters" button in the panel header
    Then no filter group should display a count badge
    And the "Clear filters" button should no longer be shown

  ═══════════════════════════════════════════════════════════════════════════
  Rule: Settings changes preview immediately and are discarded unless saved
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: Changing "Column by" previews immediately without saving
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Importance" in the "Column by" dropdown
    Then the view should immediately reorganise columns by importance
    And nothing should be persisted to the server

  Scenario: Closing the popup without saving discards all unsaved changes
    When the user clicks the "View settings" button in the view toolbar
    And the user changes "Sort by" to "Title"
    And the user clicks outside the popup or presses Escape
    Then the settings popup should close
    And the view should revert to the settings as they were before the popup was opened
    And reopening the panel should show "Sort by" as "Manual"

  Scenario: Previewing a setting then navigating to another view discards the preview
    When the user clicks the "View settings" button in the view toolbar
    And the user changes "Column by" to "Assignee" without saving
    And the user clicks another view tab to switch views
    Then the settings panel should close
    And the original view should revert to its saved settings

  ═══════════════════════════════════════════════════════════════════════════
  Rule: A views.write holder can save settings for everyone or only for themselves
  ═══════════════════════════════════════════════════════════════════════════

  Background:
    Given the user has navigated to the Product Backlog of "E2E_VS_PROJECT"

  Scenario: The footer offers "Save for everyone" plus a "Save only for me" menu entry
    When the user clicks the "View settings" button in the view toolbar
    Then the panel footer should contain a "Save for everyone" button
    And the panel footer should contain a dropdown trigger next to it
    And opening that dropdown should offer a "Save only for me" entry
    And the panel footer should not contain a plain "Save" button

  Scenario: Both save actions are disabled until the draft differs
    When the user clicks the "View settings" button in the view toolbar
    Then the "Save for everyone" button should be disabled
    And the "Save only for me" entry should be disabled
    When the user selects "Title" in the "Sort by" dropdown
    Then the "Save for everyone" button should be enabled
    And the "Save only for me" entry should be enabled

  Scenario: "Save for everyone" publishes the settings as the team default
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Title" in the "Sort by" dropdown
    And the user clicks "Save for everyone"
    Then the settings panel should close
    And the view's shared "Sort by" setting should be "Title"
    And the view should not be marked personalized for the user
    And reopening the panel should not show the "Only visible to you" badge

  Scenario: "Save only for me" keeps the settings personal
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Title" in the "Sort by" dropdown
    And the user opens the save dropdown and clicks "Save only for me"
    Then the settings panel should close
    And the view's shared "Sort by" setting should be unchanged
    And the user's own "Sort by" setting for the view should be "Title"
    And reopening the panel should show the "Only visible to you" badge

  Scenario: Another member never sees a personal-only save
    Given the user has saved "Sort by: Title" with "Save only for me"
    When another member of the project opens the same view
    Then their "Sort by" setting should still be "Manual"
    And they should not see the "Only visible to you" badge

  Scenario: "Save for everyone" clears the publisher's own personal override
    Given the user has saved "Sort by: Title" with "Save only for me"
    When the user reopens the panel and changes "Sort by" to "Created"
    And the user clicks "Save for everyone"
    Then the view's shared "Sort by" setting should be "Created"
    And the user's personal override should be gone
    And reopening the panel should not show the "Only visible to you" badge

  Scenario: Reset reverts the draft to the team default, not the last personal save
    Given the shared "Sort by" setting is "Manual"
    And the user has saved "Sort by: Title" with "Save only for me"
    When the user reopens the panel
    Then the "Sort by" dropdown should show "Title"
    When the user clicks the "Reset" button
    Then the "Sort by" dropdown should show "Manual"
    And the personal override should still be stored until the user saves
    And the "Reset" button should no longer be shown

  ═══════════════════════════════════════════════════════════════════════════
  Rule: A member without views.write can only save personal settings
  ═══════════════════════════════════════════════════════════════════════════

  Background:
    Given a user "E2E_VS_VIEWER" is a member of "E2E_VS_PROJECT" with the "Viewer" role (views.read only)
    And the user is signed in as "E2E_VS_VIEWER" instead of the admin
    And the user has navigated to the Product Backlog of "E2E_VS_PROJECT"

  Scenario: The footer shows a single "Save" button instead of the split button
    When the user clicks the "View settings" button in the view toolbar
    Then the panel footer should contain a "Save" button
    And the panel footer should not contain a "Save for everyone" button
    And the panel footer should not contain a dropdown trigger

  Scenario: "Save" is disabled until the draft differs
    When the user clicks the "View settings" button in the view toolbar
    Then the "Save" button should be disabled
    When the user selects "Title" in the "Sort by" dropdown
    Then the "Save" button should be enabled

  Scenario: "Save" stores a personal override and never touches the shared view
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Title" in the "Sort by" dropdown
    And the user clicks "Save"
    Then the settings panel should close
    And the view's shared "Sort by" setting should be unchanged
    And the user's own "Sort by" setting for the view should be "Title"
    And reopening the panel should show the "Only visible to you" badge

  Scenario: Reset reverts to the team default
    Given the user has saved "Sort by: Title" with "Save"
    When the user reopens the panel
    And the user clicks the "Reset" button
    Then the "Sort by" dropdown should show the team default "Manual"

  ═══════════════════════════════════════════════════════════════════════════
  Rule: Column by works in both Board and Table view layouts
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: Board view respects Column by setting
    Given the user is on the Board view
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Assignee" in the "Column by" dropdown
    And the user saves the view settings
    Then the board should render one kanban column per assignee
    And each column should show tasks assigned to that person

  Scenario: Table view respects Column by setting as group headings
    Given the user is on the Table view
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Importance" in the "Column by" dropdown
    And the user saves the view settings
    Then the table should display group headings for each importance level
    And tasks should be grouped under their corresponding importance heading

  Scenario: Column by with custom field works in Board view
    Given the project has a custom field "Severity" of type "select" with options "Low,Medium,High"
    And the user is on the Board view
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Severity" in the "Column by" dropdown
    And the user saves the view settings
    Then the board should render one column per "Severity" option
    And all severity columns should be visible even if empty

  Scenario: Column by with custom field works in Table view
    Given the project has a custom field "Severity" of type "select" with options "Low,Medium,High"
    And the user is on the Table view
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Severity" in the "Column by" dropdown
    And the user saves the view settings
    Then the table should display group headings for "Low", "Medium", and "High"
    And tasks should be grouped under their "Severity" value

  ═══════════════════════════════════════════════════════════════════════════
  Rule: Dragging tasks between columns updates the underlying field value
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: Dragging a task between status columns updates the task's status
    Given the view is configured with "Column by: Status"
    And the user has the "tasks.write" project permission
    And the interaction has a task "E2E_DRAG_STATUS" in column "Todo"
    When the user drags "E2E_DRAG_STATUS" to the "In Progress" column
    Then the task's status should be updated to "In Progress"

  Scenario: Dragging a task between importance columns updates the task's importance
    Given the view is configured with "Column by: Importance"
    And the user has the "tasks.write" project permission
    And the interaction has a task "E2E_DRAG_IMP" in column "Low"
    When the user drags "E2E_DRAG_IMP" to the "High" column
    Then the task's importance should be updated to 3 (High)

  Scenario: Dragging a task between assignee columns updates the task's assignee
    Given the view is configured with "Column by: Assignee"
    And the user has the "tasks.write" project permission
    And the interaction has a task "E2E_DRAG_ASSIGNEE" in column "E2E_ALICE"
    When the user drags "E2E_DRAG_ASSIGNEE" to the "E2E_BOB" column
    Then the task's assignee should be updated to "E2E_BOB"

  Scenario: Dragging a task between custom select field columns updates the field
    Given the project has a custom field "Severity" of type "select" with options "Low,Medium,High"
    And the view is configured with "Column by: Severity"
    And the user has the "tasks.write" project permission
    And the interaction has a task "E2E_DRAG_SEV" with custom field "Severity" set to "Low"
    When the user drags "E2E_DRAG_SEV" to the "High" column
    Then the task's "Severity" custom field should be updated to "High"

  ═══════════════════════════════════════════════════════════════════════════
  Rule: Newly added custom fields appear in all view setting dropdowns
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: Creating a new custom field makes it available in Column by
    Given the project initially has no custom fields
    And the user creates a custom field "Team" of type "select" with options "Alpha,Bravo,Charlie"
    When the user clicks the "View settings" button in the view toolbar
    Then the "Column by" dropdown should include the option "Team"

  Scenario: Creating a new numeric custom field makes it available in Sort by and Field sum
    Given the project initially has no custom fields
    And the user creates a custom field "Complexity" of type "number"
    When the user clicks the "View settings" button in the view toolbar
    Then the "Sort by" dropdown should include the option "Complexity"
    And the "Field sum" dropdown should include the option "Complexity"

  Scenario: Deleting a custom field removes it from all view setting dropdowns
    Given the project has a custom field "Obsolete Field" of type "text"
    And the "Obsolete Field" is listed in the "Column by" dropdown
    When the user deletes the "Obsolete Field" custom field
    And the user reopens the view settings panel
    Then the "Column by" dropdown should no longer include the option "Obsolete Field"

  ═══════════════════════════════════════════════════════════════════════════
  Rule: Custom fields with missing values are handled gracefully
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: Tasks without a value for a custom field appear in an "Unset" or "None" column
    Given the project has a custom field "Severity" of type "select" with options "Low,Medium,High"
    And the interaction has a task "E2E_NO_SEV" without the "Severity" custom field set
    And the interaction has a task "E2E_HAS_SEV" with custom field "Severity" set to "High"
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Severity" in the "Column by" dropdown
    And the user saves the view settings
    Then the view should display columns for "Low", "Medium", "High", and "None" (or "Unset")
    And the "None" (or "Unset") column should contain the task "E2E_NO_SEV"
    And the "High" column should contain the task "E2E_HAS_SEV"

  Scenario: Sorting by a custom field where some tasks lack the field places them last
    Given the project has a custom field "Story Points" of type "number"
    And the interaction has a task "E2E_HAS_SP" with custom field "Story Points" set to 5
    And the interaction has a task "E2E_NO_SP" without the "Story Points" custom field set
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Story Points" in the "Sort by" dropdown
    And the user saves the view settings
    Then "E2E_HAS_SP" should appear before "E2E_NO_SP" (or vice versa)
    And no error should occur

  Scenario: Field sum for a custom field ignores tasks without that field
    Given the project has a custom field "Story Points" of type "number"
    And the interaction has 2 tasks in "Todo" with "Story Points" set to 3 and 5
    And the interaction has 1 task in "Todo" without "Story Points" set
    When the user clicks the "View settings" button in the view toolbar
    And the user selects "Story Points" in the "Field sum" dropdown
    And the user saves the view settings
    Then the "Todo" group heading should display a total of "8" story points
