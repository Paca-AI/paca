@projects @interactions @timeline
Feature: Timeline interaction (Epic roadmap)
  The Timeline is a dedicated project interaction accessible from the project
  sidebar under the "Interactions" section, appearing above "Product Backlog"
  and all sprint entries.  Its purpose is long-horizon planning of Epics on a
  Gantt-style roadmap.  When a user opens the Timeline the page header shows
  "Timeline" with the subtitle "Epics and long-horizon planning on a roadmap."
  The default and only pre-configured view uses the Roadmap layout.  The
  Timeline view settings pre-filter to show only the "Epic" system task type;
  normal task types (Story, Bug, Task, etc.) are excluded by default.  The
  timeline canvas renders a month-based horizontal bar chart — each visible
  Epic appears as a bar spanning its start-date to due-date range.  When no
  Epics exist an empty-state message "No tasks to display" is shown.  An
  "Add task" button at the bottom of the canvas creates new Epic tasks inline.
  Users may add Board or Table views to the Timeline interaction in addition
  to the default Roadmap view.  The view settings "Column by" dropdown
  defaults to "Status" (not "Sprint") for the Timeline interaction.  The
  Timeline interaction does not show any sprint-related controls (no "New
  sprint" button, no sprint columns, no "Start sprint" button).

  @authenticated
  Rule: Timeline interaction page header and navigation

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_TIMELINE_PROJECT" exists
      And the user has the "View Sprints" project permission in "E2E_TIMELINE_PROJECT"
      And the user has navigated to the "E2E_TIMELINE_PROJECT" project

    Scenario: Timeline appears as the first entry in the Interactions sidebar section
      Then the project sidebar should contain a "Timeline" entry in the Interactions section
      And the "Timeline" entry should appear above "Product Backlog" and any sprint entries

    Scenario: Clicking Timeline in the sidebar opens the Timeline interaction page
      When the user clicks "Timeline" in the project sidebar
      Then the page URL should contain "/interactions/timeline"
      And the "Timeline" sidebar entry should be marked as active

    Scenario: Timeline page heading is "Timeline"
      When the user navigates to the Timeline interaction
      Then the page heading should display "Timeline"

    Scenario: Timeline page subtitle reads "Epics and long-horizon planning on a roadmap."
      When the user navigates to the Timeline interaction
      Then the page subtitle should read "Epics and long-horizon planning on a roadmap."

    Scenario: Timeline does not show a "New sprint" button
      When the user navigates to the Timeline interaction
      Then no "New sprint" button should be visible on the Timeline page

  @authenticated
  Rule: Timeline default Roadmap view

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_TIMELINE_VIEW_PROJECT" exists
      And the user has the "View Sprints" project permission in "E2E_TIMELINE_VIEW_PROJECT"
      And the user has navigated to the Timeline interaction inside "E2E_TIMELINE_VIEW_PROJECT"

    Scenario: Timeline opens with the Roadmap view active by default
      Then the "Roadmap" view tab should be active
      And the Roadmap (Gantt-style timeline) layout should be displayed

    Scenario: Timeline Roadmap view shows a month-based horizontal axis
      Then the timeline canvas should display at least one month label on the horizontal axis

    Scenario: Timeline empty state shows "No tasks to display" when no Epics exist
      Given the project has no Epic tasks
      Then the message "No tasks to display" should be visible in the timeline canvas

    Scenario: An "Add task" button is visible at the bottom of the Timeline canvas
      Then an "Add task" button should be present below the timeline canvas

    Scenario: Timeline Roadmap view shows a task list panel on the left side
      Then a left-side panel with the label "Task" should be visible alongside the timeline canvas

    Scenario: Each Epic appears as a horizontal bar spanning its scheduled date range
      Given the project has an Epic named "E2E_EPIC_BAR" with start date "2026-04-01" and due date "2026-04-30"
      Then a bar labelled "E2E_EPIC_BAR" should be visible in the April column range of the timeline
      And the bar should span from the start date to the due date

    Scenario: Epics without dates appear in the task list but show no bar on the canvas
      Given the project has an Epic named "E2E_EPIC_NO_DATES" with no start date and no due date
      Then "E2E_EPIC_NO_DATES" should appear in the left-side task list
      And no bar should be rendered for "E2E_EPIC_NO_DATES" on the timeline canvas

  @authenticated
  Rule: Timeline pre-filters to Epic task types only

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_TIMELINE_FILTER_PROJECT" exists
      And the project has tasks of types "Epic", "Story", "Bug", and "Task"
      And the user has the "View Sprints" project permission in "E2E_TIMELINE_FILTER_PROJECT"
      And the user has navigated to the Timeline interaction inside "E2E_TIMELINE_FILTER_PROJECT"

    Scenario: Timeline view settings default to showing only Epic task type
      When the user opens the View settings panel
      Then under "Task types" the "Epic" checkbox should be checked
      And the "Story", "Bug", and "Task" type checkboxes should be unchecked

    Scenario: Only Epics appear in the Timeline Roadmap view by default
      Given the project has an Epic "E2E_TL_EPIC" and a Story "E2E_TL_STORY"
      Then "E2E_TL_EPIC" should appear in the timeline task list
      And "E2E_TL_STORY" should not appear in the timeline task list

    Scenario: Adding a normal task type in view settings makes those tasks appear
      When the user opens the View settings panel
      And the user checks the "Story" task type checkbox
      And the user clicks "Save"
      Then tasks of type "Story" should appear in the timeline task list alongside Epics

  @authenticated
  Rule: Timeline view settings

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_TIMELINE_SETTINGS_PROJECT" exists
      And the user has the "View Sprints" project permission in "E2E_TIMELINE_SETTINGS_PROJECT"
      And the user has navigated to the Timeline interaction inside "E2E_TIMELINE_SETTINGS_PROJECT"

    Scenario: Timeline view settings "Column by" defaults to "Status"
      When the user opens the View settings panel
      Then the "Column by" dropdown trigger should display "Status"

    Scenario: Timeline view settings "Column by" offers Sprint as an option
      When the user opens the View settings panel
      Then the "Column by" dropdown should include the option "Sprint"

    Scenario: Timeline view settings panel contains a Sprints filter
      When the user opens the View settings panel
      Then the filters section should contain a "Sprints" filter button

    Scenario: Timeline view settings panel contains a Statuses filter
      When the user opens the View settings panel
      Then the filters section should contain a "Statuses" filter button

    Scenario: View settings panel has "Save" and "Reset" buttons
      When the user opens the View settings panel
      Then the panel should contain a "Save" button and a "Reset" button

    Scenario: "Clear filters" button is active when a filter is applied
      Given the user has applied a filter in the Timeline view settings
      Then the "Clear filters" button should be active (enabled) in the settings panel header

  @authenticated
  Rule: Adding additional views to the Timeline interaction

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_TIMELINE_ADDVIEW_PROJECT" exists
      And the user has the "Manage Views" project permission in "E2E_TIMELINE_ADDVIEW_PROJECT"
      And the user has navigated to the Timeline interaction inside "E2E_TIMELINE_ADDVIEW_PROJECT"

    Scenario: An "Add view" button is visible to the right of the last view tab
      Then an "Add view" button should be visible immediately to the right of the last view tab

    Scenario: Clicking "Add view" opens a view creation dialog with Board, Table, and Roadmap options
      When the user clicks the "Add view" button
      Then a dialog should appear offering "Board", "Table", and "Roadmap" as layout options
      And a "View name" field should be pre-filled with a default name

    Scenario: Creating a Board view on the Timeline adds a new tab
      When the user clicks the "Add view" button
      And the user enters the name "E2E_TIMELINE_BOARD_VIEW" and selects "Board" layout
      And the user clicks "Create view"
      Then the "E2E_TIMELINE_BOARD_VIEW" tab should appear in the view tab bar
      And clicking the tab should display a kanban board layout

    Scenario: Creating a Table view on the Timeline adds a new tab
      When the user clicks the "Add view" button
      And the user enters the name "E2E_TIMELINE_TABLE_VIEW" and selects "Table" layout
      And the user clicks "Create view"
      Then the "E2E_TIMELINE_TABLE_VIEW" tab should appear in the view tab bar
      And clicking the tab should display a tabular list layout

    Scenario: User without "Manage Views" permission does not see the "Add view" button
      Given the user does not have the "Manage Views" project permission
      Then no "Add view" button should be visible in the Timeline view tab bar

  @authenticated
  Rule: Adding Epic tasks from the Timeline

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_TIMELINE_ADDTASK_PROJECT" exists
      And the user has the "Create Tasks" project permission in "E2E_TIMELINE_ADDTASK_PROJECT"
      And the user has navigated to the Timeline interaction inside "E2E_TIMELINE_ADDTASK_PROJECT"

    Scenario: Clicking "Add task" from the Timeline canvas opens an inline task creation input
      When the user clicks the "Add task" button below the timeline canvas
      Then an inline task creation input should appear

    Scenario: A task created from the Timeline canvas defaults to Epic type
      When the user clicks "Add task" and types "E2E_INLINE_EPIC" and presses Enter
      Then a task named "E2E_INLINE_EPIC" should be created with type "Epic"
      And "E2E_INLINE_EPIC" should appear in the Timeline task list

    Scenario: User without "Create Tasks" permission does not see the "Add task" button
      Given the user does not have the "Create Tasks" project permission
      Then the "Add task" button should not be visible on the Timeline canvas
