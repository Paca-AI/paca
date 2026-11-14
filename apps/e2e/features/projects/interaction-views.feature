@projects @interactions @views
Feature: Interaction views (board and list layouts)
  Each project interaction (product backlog or a sprint) can be explored
  through one or more saved views.  A view defines a named layout — Table,
  Board, or Gantt — and persists the user's preferred way of looking at work
  items.  Each view also stores display settings (visible fields, column
  grouping, swimlanes, sort order, field sum, and slice dimension).  The
  default view opened when entering the product backlog is the Table view
  grouped by sprint (column_by = "sprint"); for sprint interactions the default
  view is the Board view (kanban layout grouped by status).  On the product
  backlog Table view every sprint column header shows a "Start sprint" button
  for planned sprints, allowing the user to open the Start Sprint modal without
  leaving the backlog.  The
  product backlog page header and each sprint-column header also contain a
  "New sprint" button that quick-creates a sprint with a system-generated
  default name (no modal).  Epic and Subtask tasks are excluded from both
  the product backlog and sprint views.  Users with sufficient permissions
  may create additional views, rename or delete them, and switch freely
  between layouts.  An "Add view" (+) button sits to the right of the last
  view tab; a separate "View settings" button in the view toolbar opens a
  settings panel for the active view.  Creating tasks is available inline
  from both Table and Board views.

  @authenticated
  Rule: Entering an interaction opens its default view

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_VIEWS_PROJECT" exists
      And the project has a "Product Backlog" interaction with at least one view
      And the user has the "View Sprints" project permission in "E2E_VIEWS_PROJECT"
      And the user has navigated to the "E2E_VIEWS_PROJECT" project

    Scenario: Navigating to the product backlog opens the default Table view
      When the user clicks "Product Backlog" in the project sidebar
      Then the interaction page for "Product Backlog" should be visible
      And a view tab bar should be shown below the interaction header
      And the "Table" view tab should be active
      And the tabular list layout should be displayed

    Scenario: The product backlog default Table view groups tasks by sprint
      When the user clicks "Product Backlog" in the project sidebar
      Then the table should be grouped into sprint columns
      And each sprint column should display the sprint name as its header
      And a "Backlog" column should be visible for tasks with no sprint assigned

    Scenario: The view header shows the interaction name and a description
      When the user clicks "Product Backlog" in the project sidebar
      Then the page header should display "Product Backlog"
      And a subtitle or description area should be visible beneath the header

    Scenario: Board view is indicated by its tab label
      Given the "Product Backlog" interaction has a view named "Board" with layout "Board"
      When the user clicks "Product Backlog" in the project sidebar
      And the user clicks the "Board" view tab
      Then the kanban board layout should be displayed
      And each status column should be visible as a swimlane

    Scenario: Table view is indicated by its tab label
      Given the "Product Backlog" interaction has a view named "Table" with layout "Table"
      When the user clicks "Product Backlog" in the project sidebar
      And the user clicks the "Table" view tab
      Then the tabular list layout should be displayed
      And each task should appear as a row with its title, status, type, and assignee

    Scenario: Gantt view is indicated by its tab label
      Given the "Product Backlog" interaction has a view named "Gantt" with layout "Gantt"
      When the user clicks "Product Backlog" in the project sidebar
      And the user clicks the "Gantt" view tab
      Then the Gantt timeline layout should be displayed
      And each task should appear as a horizontal bar spanning its scheduled date range

    Scenario: Navigating to a sprint opens the default Board view
      Given the project has an active sprint named "E2E_SPRINT_1"
      When the user clicks "E2E_SPRINT_1" in the project sidebar
      Then the interaction page for "E2E_SPRINT_1" should be visible
      And the view tab bar should be shown
      And the "Board" view tab should be active
      And the kanban board layout should be displayed grouped by status

  @authenticated
  Rule: Product backlog Table view sprint-column headers

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_BACKLOG_SPRINT_COLS_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Table" view
      And the project has a planned sprint named "E2E_PLANNED_SPRINT_COL"
      And the project has an active sprint named "E2E_ACTIVE_SPRINT_COL"
      And the user has the "View Sprints" project permission in "E2E_BACKLOG_SPRINT_COLS_PROJECT"
      And the user has the "Manage Sprints" project permission in "E2E_BACKLOG_SPRINT_COLS_PROJECT"
      And the user has navigated to the "Product Backlog" table view inside "E2E_BACKLOG_SPRINT_COLS_PROJECT"

    Scenario: Each sprint appears as a named column in the product backlog table view
      Then a column header named "E2E_PLANNED_SPRINT_COL" should be visible
      And a column header named "E2E_ACTIVE_SPRINT_COL" should be visible
      And a "Backlog" column header should be visible for tasks with no sprint

    Scenario: A "Start sprint" button appears in the column header of a planned sprint
      Then the column header for "E2E_PLANNED_SPRINT_COL" should contain a "Start sprint" button

    Scenario: The "Start sprint" button is not present on an already-active sprint column
      Then the column header for "E2E_ACTIVE_SPRINT_COL" should not contain a "Start sprint" button

    Scenario: Clicking "Start sprint" in a column header opens the Start Sprint inline form
      When the user clicks "Start sprint" in the "E2E_PLANNED_SPRINT_COL" column header
      Then the Start sprint inline form should open within the sprint column
      And the form should display the sprint name "E2E_PLANNED_SPRINT_COL" in an editable "Name" field
      And the form should contain an optional "Goal" field
      And the form should contain a "Start date" field pre-populated with today's date
      And the form should contain an optional "End date" field
      And the form should contain a "Start sprint" submit button
      And the form should contain a "Cancel" button

    Scenario: Submitting the Start Sprint inline form transitions the sprint to active
      When the user clicks "Start sprint" in the "E2E_PLANNED_SPRINT_COL" column header
      And the user fills the goal with "Deliver login feature"
      And the user sets the start date to "2026-04-14"
      And the user sets the end date to "2026-04-27"
      And the user clicks "Start sprint" in the form
      Then the inline form should close
      And the browser should navigate to the "E2E_PLANNED_SPRINT_COL" sprint interaction page
      And the "Start sprint" button should no longer appear on that column header

  @authenticated
  Rule: New sprint quick-create from product backlog page header and sprint columns

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_NEW_SPRINT_BTN_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Table" view
      And the user has the "View Sprints" project permission in "E2E_NEW_SPRINT_BTN_PROJECT"
      And the user has the "Manage Sprints" project permission in "E2E_NEW_SPRINT_BTN_PROJECT"
      And the user has navigated to the "Product Backlog" table view inside "E2E_NEW_SPRINT_BTN_PROJECT"

    Scenario: A "New sprint" button is visible in the product backlog page header
      Then the product backlog page header should contain a "New sprint" button

    Scenario: Clicking "New sprint" in the page header quick-creates a sprint with a default name
      When the user clicks "New sprint" in the product backlog page header
      Then a new sprint should be created with a system-generated name matching "Sprint \d+"
      And no modal or dialog should be shown
      And the new sprint column should appear in the table view

    Scenario: Each sprint column header also contains a "New sprint" quick-create button
      Then each sprint column header should contain a "New sprint" quick-create button

    Scenario: Clicking "New sprint" in a sprint column header quick-creates a sprint
      When the user clicks "New sprint" in any sprint column header
      Then a new sprint should be created with a system-generated name
      And the new sprint column should appear in the table view without a modal

    Scenario: "New sprint" button is not visible to users without "Manage Sprints" permission
      Given the user does not have the "Manage Sprints" project permission
      Then the "New sprint" button should not be visible in the product backlog page header

  @authenticated
  Rule: Epic and Subtask tasks are excluded from product backlog and sprint views

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_EPIC_FILTER_PROJECT" exists
      And the project has a "Product Backlog" interaction
      And the project has tasks of various types including "Epic", "Subtask", "Story", and "Bug"
      And the user has the "View Sprints" project permission in "E2E_EPIC_FILTER_PROJECT"
      And the user has navigated to the "Product Backlog" interaction inside "E2E_EPIC_FILTER_PROJECT"

    Scenario: Epic tasks do not appear in the product backlog table view
      Given there is a task named "E2E_EPIC_TASK" with type "Epic" in the product backlog
      When the user views the "Table" layout
      Then no task named "E2E_EPIC_TASK" should be visible in any column

    Scenario: Subtask tasks do not appear in the product backlog table view
      Given there is a task named "E2E_SUBTASK_TASK" with type "Subtask" in the product backlog
      When the user views the "Table" layout
      Then no task named "E2E_SUBTASK_TASK" should be visible in any column

    Scenario: Non-system task types still appear in the product backlog
      Given there is a task named "E2E_STORY_TASK" with type "Story" in the product backlog
      And there is a task named "E2E_BUG_TASK" with type "Bug" in the product backlog
      When the user views the "Table" layout
      Then the task "E2E_STORY_TASK" should be visible
      And the task "E2E_BUG_TASK" should be visible

    Scenario: Epic tasks do not appear in a sprint's board view
      Given the project has an active sprint named "E2E_EPIC_SPRINT"
      And the sprint "E2E_EPIC_SPRINT" has a task "E2E_EPIC_IN_SPRINT" with type "Epic"
      When the user navigates to the "E2E_EPIC_SPRINT" board view
      Then no task named "E2E_EPIC_IN_SPRINT" should be visible in any column

    Scenario: Subtask tasks do not appear in a sprint's table view
      Given the project has an active sprint named "E2E_SUBTASK_SPRINT"
      And the sprint "E2E_SUBTASK_SPRINT" has a task "E2E_SUBTASK_IN_SPRINT" with type "Subtask"
      When the user navigates to the "E2E_SUBTASK_SPRINT" table view
      Then no task named "E2E_SUBTASK_IN_SPRINT" should be visible in any row

  @authenticated
  Rule: Board view layout and task display

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_BOARD_PROJECT" exists
      And the project has a "Product Backlog" interaction
      And the interaction has a "Board" view
      And the user has the "View Sprints" project permission in "E2E_BOARD_PROJECT"
      And the user has navigated to the "Product Backlog" board view inside "E2E_BOARD_PROJECT"

    Scenario: Board columns match the project's configured task statuses
      Then a column should exist for each configured task status
      And the column headers should display the status name and task count

    Scenario: Tasks appear in the column matching their current status
      Given the interaction has a task "E2E_TASK_TODO" with status "Todo"
      And the interaction has a task "E2E_TASK_IN_PROGRESS" with status "In Progress"
      Then the "Todo" column should contain the task card "E2E_TASK_TODO"
      And the "In Progress" column should contain the task card "E2E_TASK_IN_PROGRESS"

    Scenario: Each task card shows title, task type badge, and assignee avatar
      Given the interaction has a task "E2E_CARD_TASK" with type "Story" assigned to "E2E_USER"
      Then the card for "E2E_CARD_TASK" should display the task title "E2E_CARD_TASK"
      And the card should show the task type badge "Story"
      And the card should show the assignee avatar for "E2E_USER"

    Scenario: Task cards with no assignee show an empty avatar placeholder
      Given the interaction has an unassigned task "E2E_UNASSIGNED_TASK"
      Then the card for "E2E_UNASSIGNED_TASK" should show an unassigned icon

    Scenario: Columns with no tasks show an empty-state message
      Given the "Done" column contains no tasks
      Then the "Done" column should display an empty-state prompt

    Scenario: Clicking a task card opens the task detail panel or page
      Given the interaction has a task "E2E_DETAIL_TASK"
      When the user clicks the card for "E2E_DETAIL_TASK"
      Then the task detail panel or page for "E2E_DETAIL_TASK" should open

  @authenticated
  Rule: Dragging tasks between board columns changes their status

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_DRAG_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Board" view
      And the user has the "Edit Tasks" project permission in "E2E_DRAG_PROJECT"
      And the user has navigated to the "Product Backlog" board view inside "E2E_DRAG_PROJECT"

    Scenario: Dragging a task card to another column updates the task status
      Given the interaction has a task "E2E_DRAG_TASK" in column "Todo"
      When the user drags "E2E_DRAG_TASK" to the "In Progress" column
      Then the task "E2E_DRAG_TASK" should appear in the "In Progress" column
      And the task's status should be updated to "In Progress"

    Scenario: Dropping a task back to its original column reverts the status
      Given the interaction has a task "E2E_REVERT_TASK" in column "In Progress"
      When the user drags "E2E_REVERT_TASK" to the "In Progress" column and drops it there
      Then the task "E2E_REVERT_TASK" should remain in the "In Progress" column

    Scenario: User without "Edit Tasks" permission cannot drag task cards
      Given the user does not have the "Edit Tasks" project permission
      And the interaction has a task "E2E_READONLY_TASK" in column "Todo"
      When the user attempts to drag "E2E_READONLY_TASK" to "In Progress"
      Then the drag operation should not be permitted
      And "E2E_READONLY_TASK" should remain in the "Todo" column

  @authenticated
  Rule: Table view layout and task display

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_TABLE_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Table" view
      And the user has the "View Sprints" project permission in "E2E_TABLE_PROJECT"
      And the user has navigated to the "Product Backlog" table view inside "E2E_TABLE_PROJECT"

    Scenario: Table view renders tasks as rows grouped by status
      Then tasks should be displayed as rows grouped under their status heading
      And each group heading should show the status name and task count
      And each row should display the task title

    Scenario: Each table row shows the standard column set
      Given the interaction has at least one task
      Then each task row should have visible columns for "ID", "Title", "Assignee", "Importance", and "Type"

    Scenario: Status groups in the table can be collapsed and expanded
      Given a status group "In Progress" is expanded and has tasks
      When the user clicks the "In Progress" group header toggle
      Then the "In Progress" group should collapse and hide its task rows
      When the user clicks the "In Progress" group header toggle again
      Then the "In Progress" group should expand and show its task rows

    Scenario: Clicking a task row opens the task detail panel or page
      Given the interaction has a task "E2E_TABLE_DETAIL_TASK"
      When the user clicks the row for "E2E_TABLE_DETAIL_TASK"
      Then the task detail panel or page for "E2E_TABLE_DETAIL_TASK" should open

    Scenario: Completed task groups are collapsed by default
      Given the interaction has tasks with status "Done"
      Then the "Done" group should be collapsed by default
      And the collapsed heading should show the task count

  @authenticated
  Rule: Creating a task from the board view

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_CREATE_BOARD_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Board" view
      And the user has the "Create Tasks" project permission in "E2E_CREATE_BOARD_PROJECT"
      And the user has navigated to the "Product Backlog" board view inside "E2E_CREATE_BOARD_PROJECT"

    Scenario: Each board column has an "Add task" button at the bottom
      Then every status column should show an "Add task" button at its bottom

    Scenario: Clicking "Add task" in a column opens an inline creation control
      When the user clicks "Add task" in the "Todo" column
      Then an inline task creation input should appear at the bottom of the "Todo" column

    Scenario: Typing a title and pressing Enter creates the task in that column
      When the user clicks "Add task" in the "Todo" column
      And the user types "E2E_BOARD_NEW_TASK" in the inline task input
      And the user presses Enter
      Then a task card named "E2E_BOARD_NEW_TASK" should appear in the "Todo" column
      And the inline input should close

    Scenario: Pressing Escape cancels inline task creation without creating a task
      When the user clicks "Add task" in the "Todo" column
      And the user types "E2E_CANCELLED_TASK" in the inline task input
      And the user presses Escape
      Then no task named "E2E_CANCELLED_TASK" should appear in the "Todo" column
      And the inline input should close

    Scenario: Submitting an empty task title does not create a task
      When the user clicks "Add task" in the "Backlog" column
      And the user presses Enter without typing a title
      Then no new task should be added to the "Backlog" column
      And the inline input should remain open or show a validation hint

    Scenario: User without "Create Tasks" permission does not see "Add task" buttons
      Given the user does not have the "Create Tasks" project permission
      Then no "Add task" button should be visible on any column

  @authenticated
  Rule: Creating a task from the table view

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_CREATE_TABLE_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Table" view
      And the user has the "Create Tasks" project permission in "E2E_CREATE_TABLE_PROJECT"
      And the user has navigated to the "Product Backlog" table view inside "E2E_CREATE_TABLE_PROJECT"

    Scenario: Each status group in the table view has an "Add task" button
      Then every status group should show an "Add task" button below the last row

    Scenario: Clicking "Add task" in a group opens an inline creation row
      When the user clicks "Add task" in the "Todo" group
      Then an inline task creation row should appear at the bottom of the "Todo" group

    Scenario: Typing a title and pressing Enter creates the task in that group
      When the user clicks "Add task" in the "Backlog" group
      And the user types "E2E_TABLE_NEW_TASK" in the inline task row
      And the user presses Enter
      Then a row named "E2E_TABLE_NEW_TASK" should appear in the "Backlog" group
      And the inline row should close

    Scenario: Pressing Escape cancels inline creation in the table view
      When the user clicks "Add task" in the "Todo" group
      And the user types "E2E_TABLE_CANCELLED_TASK" in the inline task row
      And the user presses Escape
      Then no task named "E2E_TABLE_CANCELLED_TASK" should appear in the "Todo" group

    Scenario: User without "Create Tasks" permission does not see group "Add task" buttons
      Given the user does not have the "Create Tasks" project permission
      Then no "Add task" button should be visible in any status group

  @authenticated
  Rule: Managing views (create, rename, delete)

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_VM_PROJECT" exists
      And the project has a "Product Backlog" interaction
      And the user has the "Manage Views" project permission in "E2E_VM_PROJECT"
      And the user has navigated to the "Product Backlog" interaction inside "E2E_VM_PROJECT"

    Scenario: An "Add view" button is visible to the right of the last view tab for authorised users
      Then an "Add view" ("+") button should be visible immediately to the right of the last view tab

    Scenario: A "View settings" button is visible in the view toolbar for authorised users
      Then a "View settings" button should be visible in the interaction view toolbar

    Scenario: Clicking "Add view" opens a view creation popover with layout options
      When the user clicks the "Add view" button to the right of the last view tab
      Then a popover or dialog should open
      And it should offer "Table" as a layout option
      And it should offer "Board" as a layout option
      And it should offer "Gantt" as a layout option
      And it should contain a "View name" field pre-filled with a default name

    Scenario: Creating a Board view adds a new tab in the tab bar
      When the user clicks the "Add view" button to the right of the last view tab
      And the user changes the view name to "E2E_BOARD_VIEW"
      And the user selects the "Board" layout
      And the user confirms view creation
      Then a tab labelled "E2E_BOARD_VIEW" should appear in the view tab bar
      And the kanban board layout should be active

    Scenario: Creating a Table view adds a new tab in the tab bar
      When the user clicks the "Add view" button to the right of the last view tab
      And the user changes the view name to "E2E_TABLE_VIEW"
      And the user selects the "Table" layout
      And the user confirms view creation
      Then a tab labelled "E2E_TABLE_VIEW" should appear in the view tab bar
      And the tabular list layout should be active

    Scenario: Creating a Gantt view adds a new tab in the tab bar
      When the user clicks the "Add view" button to the right of the last view tab
      And the user changes the view name to "E2E_GANTT_VIEW"
      And the user selects the "Gantt" layout
      And the user confirms view creation
      Then a tab labelled "E2E_GANTT_VIEW" should appear in the view tab bar
      And the Gantt timeline layout should be active

    Scenario: Creating a view without a name defaults to a generated name
      When the user clicks the "Add view" button to the right of the last view tab
      And the user clears the view name field
      And the user selects the "Board" layout
      And the user confirms view creation
      Then a new tab should appear in the view tab bar using the generated default name

    Scenario: Renaming a view updates its tab label
      Given the interaction has an existing view tab "E2E_OLD_VIEW_NAME"
      When the user right-clicks or opens the options menu for "E2E_OLD_VIEW_NAME"
      And the user selects "Rename view"
      And the user types "E2E_RENAMED_VIEW" and confirms
      Then the tab should be relabelled "E2E_RENAMED_VIEW"

    Scenario: Deleting a view removes its tab and activates the adjacent tab
      Given the interaction has views "E2E_VIEW_ALPHA" and "E2E_VIEW_BETA"
      And the user is on the "E2E_VIEW_BETA" tab
      When the user opens the options menu for "E2E_VIEW_BETA" and selects "Delete view"
      And the user confirms the deletion
      Then the "E2E_VIEW_BETA" tab should no longer be visible
      And the "E2E_VIEW_ALPHA" tab should become active

    Scenario: The last remaining view cannot be deleted
      Given the interaction has only one view "E2E_ONLY_VIEW"
      When the user opens the options menu for "E2E_ONLY_VIEW"
      Then the "Delete view" option should be disabled or absent

    Scenario: User without "Manage Views" permission does not see the "Add view" button
      Given the user does not have the "Manage Views" project permission
      Then the "Add view" button should not be visible to the right of the view tab bar

  @authenticated
  Rule: Switching and persisting the active view

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_PERSIST_PROJECT" exists
      And the project has a "Product Backlog" interaction with views "Board" and "List"
      And the user has the "View Sprints" project permission in "E2E_PERSIST_PROJECT"

    Scenario: Clicking a view tab switches the active layout
      Given the user is on the "Board" view tab inside "Product Backlog"
      When the user clicks the "Table" view tab
      Then the tabular list layout should be displayed
      And the "Table" tab should be marked as active

    Scenario: The active view tab is visually distinguished from inactive tabs
      When the user is on the "Board" view tab
      Then the "Board" tab should have a distinct active indicator
      And the "Table" tab should not have the active indicator

    Scenario: Refreshing the page preserves the last active view
      Given the user has switched to the "List" view tab
      When the user refreshes the page
      Then the "List" view tab should still be active

  @authenticated
  Rule: Filtering and searching tasks within a view

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_FILTER_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Board" view
      And the user has the "View Sprints" project permission in "E2E_FILTER_PROJECT"
      And the user has navigated to the "Product Backlog" board view inside "E2E_FILTER_PROJECT"

    Scenario: A search or filter bar is visible at the top of the interaction view
      Then a filter or search control should be visible in the view toolbar

    Scenario: Searching by keyword filters visible tasks across all columns
      Given the interaction has tasks "E2E_ALPHA_TASK" and "E2E_BETA_TASK"
      When the user types "ALPHA" in the search bar
      Then only cards matching "ALPHA" should be visible
      And the card for "E2E_BETA_TASK" should not be visible

    Scenario: Filtering by assignee shows only tasks assigned to that user
      Given the interaction has a task "E2E_OWNED_TASK" assigned to "E2E_FILTER_USER"
      And the interaction has a task "E2E_OTHER_TASK" assigned to "E2E_OTHER_USER"
      When the user opens the filter panel and selects assignee "E2E_FILTER_USER"
      Then the card for "E2E_OWNED_TASK" should be visible
      And the card for "E2E_OTHER_TASK" should not be visible

    Scenario: Clearing the filter restores all tasks
      Given the user has applied a keyword filter "E2E_ALPHA"
      When the user clears the filter
      Then all tasks should be visible again

  @authenticated
  Rule: View settings panel

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_SETTINGS_PROJECT" exists
      And the project has a "Product Backlog" interaction with at least one view
      And the user has the "View Sprints" project permission in "E2E_SETTINGS_PROJECT"
      And the user has navigated to the "Product Backlog" interaction inside "E2E_SETTINGS_PROJECT"

    Scenario: Clicking the "View settings" button opens a settings panel
      When the user clicks the "View settings" button in the view toolbar
      Then a settings panel should appear
      And the panel should display a "Fields" row
      And the panel should display a "Column by" row
      And the panel should display a "Swimlanes" row
      And the panel should display a "Sort by" row
      And the panel should display a "Field sum" row
      And the panel should display a "Slice by" row

    Scenario: The "Fields" row shows the currently visible field names
      When the user clicks the "View settings" button in the view toolbar
      Then the "Fields" row should list the names of visible fields (e.g. "Title, Assignees, Status")

    Scenario: Changing "Column by" regroups task columns by a different field
      When the user clicks the "View settings" button in the view toolbar
      And the user changes "Column by" to "Assignee"
      Then task groups or columns should be reorganised by assignee

    Scenario: Changing "Sort by" to a field name sorts tasks automatically
      When the user clicks the "View settings" button in the view toolbar
      And the user changes "Sort by" to "Priority"
      Then tasks within each group should be ordered by priority
      And the "Sort by" row should show the value "Priority"

    Scenario: Changing "Sort by" to "Manual" shows the manual value and enables drag-to-reorder
      When the user clicks the "View settings" button in the view toolbar
      And the user sets "Sort by" to "Manual"
      Then the "Sort by" row should show the value "manual"
      And task rows should become draggable for manual reordering

    Scenario: Changing "Swimlanes" groups tasks into horizontal swimlane bands
      When the user clicks the "View settings" button in the view toolbar
      And the user changes "Swimlanes" to "Assignee"
      Then task rows or cards should be further grouped into horizontal swimlane bands by assignee

    Scenario: The "Field sum" setting defaults to "Count" showing per-group task totals
      When the user clicks the "View settings" button in the view toolbar
      Then the "Field sum" row should show the default value "Count"
      And each group heading should display the number of tasks in that group

    Scenario: Changing "Field sum" to a numeric field shows the aggregate in group headings
      When the user clicks the "View settings" button in the view toolbar
      And the user changes "Field sum" to "Story Points"
      Then each group heading should display the total story points for tasks in that group

    Scenario: The settings popup has Save and Reset buttons
      When the user clicks the "View settings" button in the view toolbar
      Then a settings popup should appear near the toolbar
      And the popup should contain a "Save" button
      And the popup should contain a "Reset" button

    Scenario: Changes in the settings popup preview immediately in the view
      When the user clicks the "View settings" button in the view toolbar
      And the user changes "Sort by" to "Manual"
      Then the view should update immediately to reflect the manual sort order
      And the change should not yet be persisted to the server

    Scenario: Clicking Save persists the settings and closes the popup
      When the user clicks the "View settings" button in the view toolbar
      And the user changes "Sort by" to "Manual"
      And the user clicks the "Save" button
      Then the settings popup should close
      And the view's "Sort by" setting should be saved as "Manual"
      And the view should still reflect the manual sort order

    Scenario: Clicking Reset reverts the draft to the last saved settings
      When the user clicks the "View settings" button in the view toolbar
      And the user changes "Sort by" to "Manual"
      And the user clicks the "Reset" button
      Then the "Sort by" field should revert to the previously saved value
      And the view should revert to reflect the previously saved settings

    Scenario: Closing the popup without saving discards unsaved changes
      When the user clicks the "View settings" button in the view toolbar
      And the user changes "Sort by" to "Manual"
      When the user clicks outside the popup or presses Escape
      Then a "Discard unsaved changes?" confirmation should appear
      When the user clicks "Discard"
      Then the settings popup should close
      And the view should revert to the settings as they were before the popup was opened

    Scenario: View settings are persisted per view
      Given the user has set "Sort by" to "Priority" on the "Board" view and saved
      When the user switches to a "Table" view and then returns to the "Board" view
      Then the "Sort by" setting on the "Board" view should still show "Priority"

  @authenticated
  Rule: Reordering view tabs by dragging

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_REORDER_PROJECT" exists
      And the project has a "Product Backlog" interaction with views "Board", "Table", and "Gantt" (in that order)
      And the user has the "Manage Views" project permission in "E2E_REORDER_PROJECT"
      And the user has navigated to the "Product Backlog" interaction inside "E2E_REORDER_PROJECT"

    Scenario: View tabs show a draggable indicator on hover
      When the user hovers over the "Table" view tab
      Then the tab should indicate it is draggable (e.g. a grab cursor or visible drag handle)

    Scenario: Dragging a view tab to a new position reorders the tab bar
      Given the view tabs are ordered "Board", "Table", "Gantt"
      When the user drags the "Gantt" tab and drops it before the "Board" tab
      Then the view tabs should be reordered to "Gantt", "Board", "Table"

    Scenario: Dropping a view tab on its original position leaves the order unchanged
      Given the view tabs are ordered "Board", "Table", "Gantt"
      When the user drags the "Table" tab and drops it back in the same position
      Then the view tabs should remain in the order "Board", "Table", "Gantt"

    Scenario: The reordered tab order persists after a page refresh
      Given the user has dragged the "Gantt" tab to the first position
      When the user refreshes the page
      Then the view tabs should still appear with "Gantt" as the first tab

    Scenario: The first tab after reordering becomes the default view for the interaction
      Given the user has dragged the "Table" tab to the first position and refreshed the page
      When the user navigates away from the interaction and returns to it
      Then the "Table" view tab should be active by default

    Scenario: User without "Manage Views" permission cannot drag view tabs
      Given the user does not have the "Manage Views" project permission
      When the user attempts to drag the "Table" view tab to a different position
      Then the drag operation should not be permitted
      And the view tab order should remain unchanged

  @authenticated
  Rule: Manual task sort order within a view

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_MANUAL_SORT_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Table" view configured with "Sort by: Manual"
      And the user has the "Edit Tasks" project permission in "E2E_MANUAL_SORT_PROJECT"
      And the user has navigated to the "Product Backlog" table view inside "E2E_MANUAL_SORT_PROJECT"

    Scenario: Table rows show a drag handle when the view sort order is manual
      Then each task row should show a drag handle icon on the left side

    Scenario: Dragging a task row reorders it within its status group
      Given the interaction has tasks "E2E_TASK_A", "E2E_TASK_B", "E2E_TASK_C" in the "Todo" group in that order
      When the user drags "E2E_TASK_A" below "E2E_TASK_C" within the "Todo" group
      Then the row order in the "Todo" group should be "E2E_TASK_B", "E2E_TASK_C", "E2E_TASK_A"

    Scenario: Manual row order persists after page refresh
      Given the user has manually reordered tasks so "E2E_FIRST_TASK" appears first in the "Todo" group
      When the user refreshes the page
      Then "E2E_FIRST_TASK" should still appear first in the "Todo" group

    Scenario: Board view with manual sort allows vertical reordering of cards within a column
      Given the "Product Backlog" has a "Board" view configured with "Sort by: Manual"
      And the "Todo" column contains tasks "E2E_BOARD_A" above "E2E_BOARD_B"
      When the user drags "E2E_BOARD_A" below "E2E_BOARD_B" within the "Todo" column
      Then "E2E_BOARD_B" should appear above "E2E_BOARD_A" in the "Todo" column

    Scenario: Manual card order in board columns persists after page refresh
      Given the "Product Backlog" has a "Board" view configured with "Sort by: Manual"
      And the user has manually placed "E2E_TOP_CARD" at the top of the "In Progress" column
      When the user refreshes the page
      Then "E2E_TOP_CARD" should still appear at the top of the "In Progress" column

    Scenario: Task rows are not manually draggable when sort order is not manual
      Given the "Product Backlog" interaction has a "Table" view configured with "Sort by: Priority"
      When the user navigates to that view
      Then task rows should not show drag handles
      And attempting to drag a task row should not reorder it

    Scenario: User without "Edit Tasks" permission cannot manually reorder tasks
      Given the user does not have the "Edit Tasks" project permission
      Then task rows in the manual-sort table view should not show drag handles

  @authenticated
  Rule: Sprint interaction page header and view defaults

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_SPRINT_PAGE_PROJECT" exists
      And the project has an active sprint named "E2E_ACTIVE_SPRINT_PAGE"
      And the user has the "View Sprints" project permission in "E2E_SPRINT_PAGE_PROJECT"
      And the user has the "Manage Sprints" project permission in "E2E_SPRINT_PAGE_PROJECT"
      And the user has navigated to the "E2E_ACTIVE_SPRINT_PAGE" sprint page inside "E2E_SPRINT_PAGE_PROJECT"

    Scenario: Sprint interaction page title shows the sprint name
      Then the page heading should display "E2E_ACTIVE_SPRINT_PAGE"

    Scenario: Sprint interaction page subtitle shows active state and start date
      Then the subtitle should contain "Active sprint"
      And the subtitle should contain the sprint's start date

    Scenario: Active sprint page shows a "Complete sprint" button in the header
      Then the page header should contain a "Complete sprint" button

    Scenario: Default view on a sprint interaction page is the Board view
      Then the "Board" view tab should be active
      And the kanban board layout should be visible

    Scenario: Sprint Board view contains a column for each project task status
      Then the board should display a column for each configured task status
      And a "Backlog" column should also be present for tasks with no status advancement

    Scenario: Sprint Board view columns each have an "Add task" inline button
      Then every status column should show an "Add task" button

    Scenario: Sprint Table view groups tasks by status with visible column headers
      When the user clicks the "Table" view tab
      Then tasks should be grouped by status heading
      And each group row should show columns "ID", "Title", "Assignee", "Importance", and "Type"

    Scenario: Switching from Board to Table view persists after page refresh
      When the user clicks the "Table" view tab
      And the user refreshes the page
      Then the "Table" view tab should still be active

  @authenticated
  Rule: Product backlog table view column structure

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_BACKLOG_COLS_PROJECT" exists
      And the project has a "Product Backlog" interaction with a "Table" view
      And the user has the "View Sprints" project permission in "E2E_BACKLOG_COLS_PROJECT"
      And the user has navigated to the "Product Backlog" table view inside "E2E_BACKLOG_COLS_PROJECT"

    Scenario: Product backlog table view columns are ID, Title, Assignee, Importance, and Type
      Given the interaction has at least one task
      Then each task row should have visible columns for "ID", "Title", "Assignee", "Importance", and "Type"

    Scenario: Product backlog table view description reads "All work items not assigned to a sprint"
      Then the page subtitle should read "All work items not assigned to a sprint."

    Scenario: Sprint columns appear before the Backlog column in the table view
      Given the project has a planned sprint "E2E_SPRINT_COL_A" and an active sprint "E2E_SPRINT_COL_B"
      Then the sprint columns for "E2E_SPRINT_COL_A" and "E2E_SPRINT_COL_B" should appear before the "Backlog" column
