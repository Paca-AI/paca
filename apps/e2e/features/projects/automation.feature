@projects @automation
Feature: Workflow automation
  Automations are per-project workflow graphs: a set of trigger, condition,
  and action nodes wired together with edges, executed by the automation
  engine whenever a trigger fires. The Automation list page
  (routes/_authenticated/projects/$projectId/automation/) shows every
  automation as a card with a status badge ("Active" or "Inactive" — there
  is no draft/archived lifecycle), and can toggle a project-wide dependency
  map showing which automations watch which predecessor tasks. Creating an
  automation immediately navigates into its graph builder
  (routes/.../automation/$automationId.tsx), which starts empty and
  inactive. The builder's header has a single Switch that flips the
  automation between "Active" and "Inactive" directly — there is no
  separate Activate/Archive/Deactivate button set, and no read-only
  "archived" state; an inactive automation's graph stays fully editable.
  The builder has two tabs: "Graph" (the node canvas plus, for
  workflows.write holders, an "Add Trigger" / "Add Condition" / "Add
  Action" node palette) and "Run history" (a list of past runs, or an
  empty state). All write actions (create, delete, node/edge editing,
  rename, activate/deactivate) are gated by the workflows.write project
  permission; viewing the list and builder at all is gated by
  workflows.read.

  @authenticated
  Rule: Automation list page — loading, empty state, and dependency map

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_AUTOMATION_PROJECT" exists
      And the user has navigated to the "E2E_AUTOMATION_PROJECT" project

    Scenario: The Automation page shows loading skeletons while automations are being fetched
      Given "E2E_AUTOMATION_PROJECT" has an inactive automation named "E2E_AUTOMATION_SLOW"
      And the automations request is slow to respond
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      Then the Automation page should display 3 loading skeleton cards and neither an empty state nor any automation card
      When the automations request completes
      Then the card for "E2E_AUTOMATION_SLOW" should be displayed in place of the skeleton cards

    Scenario: A project with no automations shows an empty state
      Given "E2E_AUTOMATION_PROJECT" has no automations
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      Then the Automation page should display an empty state with a "Create your first automation" action

    Scenario: The "New Automation" button is visible with workflows.write permission
      Given the user has the "workflows.write" project permission in "E2E_AUTOMATION_PROJECT"
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      Then the "New Automation" button should be visible

    Scenario: The "New Automation" button is hidden without workflows.write permission
      Given the user does not have the "workflows.write" project permission in "E2E_AUTOMATION_PROJECT"
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      Then the "New Automation" button should not be visible

    Scenario: The delete button on a card is hidden without workflows.write permission
      Given the user does not have the "workflows.write" project permission in "E2E_AUTOMATION_PROJECT"
      And "E2E_AUTOMATION_PROJECT" has an inactive automation named "E2E_AUTOMATION_READONLY"
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      Then the card for "E2E_AUTOMATION_READONLY" should not show a delete button

    Scenario: A project member without workflows.read permission sees the no-permission state instead of the grid
      Given the user does not have the "workflows.read" project permission in "E2E_AUTOMATION_PROJECT"
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      Then the Automation page should display the "You don't have permission to view automations" message

    Scenario: An automation card shows its name, description, status badge, and last-updated time
      Given "E2E_AUTOMATION_PROJECT" has an inactive automation named "E2E_AUTOMATION_CARD" with description "Notify on overdue tasks"
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      Then the card for "E2E_AUTOMATION_CARD" should display the description "Notify on overdue tasks"
      And the card for "E2E_AUTOMATION_CARD" should display the "Inactive" status badge
      And the card for "E2E_AUTOMATION_CARD" should display an "Updated ..." last-updated time

    Scenario: An active automation card shows the "Active" status badge
      Given "E2E_AUTOMATION_PROJECT" has an active automation named "E2E_AUTOMATION_ACTIVE_CARD"
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      Then the card for "E2E_AUTOMATION_ACTIVE_CARD" should display the "Active" status badge

    Scenario: An automation card with no description shows a placeholder
      Given "E2E_AUTOMATION_PROJECT" has an inactive automation named "E2E_AUTOMATION_NO_DESC" with no description
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      Then the card for "E2E_AUTOMATION_NO_DESC" should display the "No description" placeholder

    Scenario: Toggling the dependency map shows watched-task counts per automation
      Given "E2E_AUTOMATION_PROJECT" has an active automation named "E2E_AUTOMATION_DEP" whose "Predecessor(s) done" trigger watches 2 tasks and has a target task
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      And the user clicks the "Dependency map" button
      Then the dependency map panel should display "E2E_AUTOMATION_DEP" with "Waiting on 2 task(s)"

    Scenario: The dependency map shows an empty state when no automation watches any task
      Given "E2E_AUTOMATION_PROJECT" has no active automation with a "Predecessor(s) done" trigger that watches tasks and has a target task
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      And the user clicks the "Dependency map" button
      Then the dependency map panel should display "No predecessor dependencies configured yet"

    Scenario: Clicking the dependency map button again hides the panel
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      And the user clicks the "Dependency map" button
      And the user clicks the "Dependency map" button
      Then the dependency map panel should be hidden

    Scenario: Clicking an automation card navigates into its graph builder
      Given "E2E_AUTOMATION_PROJECT" has an inactive automation named "E2E_AUTOMATION_OPEN"
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      And the user clicks the card for "E2E_AUTOMATION_OPEN"
      Then the page URL should be the automation builder URL for "E2E_AUTOMATION_OPEN"

  @authenticated
  Rule: Creating and deleting an automation

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_AUTOMATION_CRUD_PROJECT" exists
      And the user has the "workflows.write" project permission in "E2E_AUTOMATION_CRUD_PROJECT"
      And the user has navigated to the Automation page for "E2E_AUTOMATION_CRUD_PROJECT"

    Scenario: The create dialog requires a name before it can be submitted
      When the user clicks the "New Automation" button
      Then the create automation dialog should open
      And the "Create" button should be disabled
      When the user fills the automation name with "E2E_AUTOMATION_NEW"
      Then the "Create" button should be enabled

    Scenario: Creating an automation navigates straight into its (empty, inactive) graph builder
      When the user clicks the "New Automation" button
      And the user fills the automation name with "E2E_AUTOMATION_CREATED"
      And the user fills the automation description with "Runs when a bug is filed"
      And the user clicks "Create"
      Then the create automation dialog should close
      And the page URL should be the automation builder URL for "E2E_AUTOMATION_CREATED"
      And the automation builder should show the automation as inactive
      And the automation graph canvas should be empty

    Scenario: Cancelling the create dialog discards the in-progress automation
      When the user clicks the "New Automation" button
      And the user fills the automation name with "E2E_AUTOMATION_CANCELLED"
      And the user clicks "Cancel"
      Then the create automation dialog should close
      And the Automation page should not list "E2E_AUTOMATION_CANCELLED"

    Scenario: Deleting an automation asks for confirmation before removing it
      Given "E2E_AUTOMATION_CRUD_PROJECT" has an inactive automation named "E2E_AUTOMATION_TO_DELETE"
      When the user navigates to the Automation page for "E2E_AUTOMATION_CRUD_PROJECT"
      And the user clicks the delete button on the card for "E2E_AUTOMATION_TO_DELETE"
      Then a delete confirmation dialog should appear naming "E2E_AUTOMATION_TO_DELETE"
      When the user confirms the deletion
      Then "E2E_AUTOMATION_TO_DELETE" should no longer appear on the Automation page

    Scenario: Cancelling the delete confirmation keeps the automation
      Given "E2E_AUTOMATION_CRUD_PROJECT" has an inactive automation named "E2E_AUTOMATION_KEEP"
      When the user navigates to the Automation page for "E2E_AUTOMATION_CRUD_PROJECT"
      And the user clicks the delete button on the card for "E2E_AUTOMATION_KEEP"
      And the user cancels the delete confirmation
      Then "E2E_AUTOMATION_KEEP" should still appear on the Automation page

  @authenticated
  Rule: Toggling an automation's active/inactive status

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_AUTOMATION_TOGGLE_PROJECT" exists
      And the user has the "workflows.write" project permission in "E2E_AUTOMATION_TOGGLE_PROJECT"

    Scenario: An inactive automation shows the toggle switched off
      Given "E2E_AUTOMATION_TOGGLE_PROJECT" has an inactive automation named "E2E_AUTOMATION_OFF"
      When the user opens the automation builder for "E2E_AUTOMATION_OFF"
      Then the active/inactive toggle should show "Inactive" and be switched off

    Scenario: Turning the toggle on activates the automation
      Given "E2E_AUTOMATION_TOGGLE_PROJECT" has an inactive automation named "E2E_AUTOMATION_TO_ACTIVATE"
      When the user opens the automation builder for "E2E_AUTOMATION_TO_ACTIVATE"
      And the user turns the active/inactive toggle on
      Then the active/inactive toggle should show "Active" and be switched on

    Scenario: Turning the toggle off deactivates the automation
      Given "E2E_AUTOMATION_TOGGLE_PROJECT" has an active automation named "E2E_AUTOMATION_TO_DEACTIVATE"
      When the user opens the automation builder for "E2E_AUTOMATION_TO_DEACTIVATE"
      And the user turns the active/inactive toggle off
      Then the active/inactive toggle should show "Inactive" and be switched off

    Scenario: Renaming an automation updates its name in the header
      Given "E2E_AUTOMATION_TOGGLE_PROJECT" has an inactive automation named "E2E_AUTOMATION_OLD_NAME"
      When the user opens the automation builder for "E2E_AUTOMATION_OLD_NAME"
      And the user clicks the rename (pencil) icon next to the automation name
      And the user types "E2E_AUTOMATION_NEW_NAME" into the rename field
      And the user confirms the rename
      Then the automation builder header should display "E2E_AUTOMATION_NEW_NAME"

    Scenario: The rename icon and the toggle are disabled without workflows.write permission
      Given the user does not have the "workflows.write" project permission in "E2E_AUTOMATION_TOGGLE_PROJECT"
      And "E2E_AUTOMATION_TOGGLE_PROJECT" has an inactive automation named "E2E_AUTOMATION_LOCKED"
      When the user opens the automation builder for "E2E_AUTOMATION_LOCKED"
      Then the rename (pencil) icon next to the automation name should not be visible
      And the active/inactive toggle should be disabled

  @authenticated
  Rule: Building the automation graph — palette, nodes, and run history

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_AUTOMATION_GRAPH_PROJECT" exists
      And the user has the "workflows.write" project permission in "E2E_AUTOMATION_GRAPH_PROJECT"
      And "E2E_AUTOMATION_GRAPH_PROJECT" has an inactive automation named "E2E_AUTOMATION_GRAPH"
      And the user has opened the automation builder for "E2E_AUTOMATION_GRAPH"

    Scenario: The Graph tab is shown by default with a node palette offering all three node kinds
      Then the "Graph" tab button should be displayed
      And the node palette should offer "Add Trigger", "Add Condition", and "Add Action" buttons

    Scenario: Adding a trigger node from the palette places it on the canvas and opens its config panel
      When the user adds a "Status changed" trigger node from the palette
      Then a new "Status changed" trigger node should appear on the canvas
      And the node configuration panel should open showing "Trigger" and "Status changed"

    Scenario: Saving a trigger node's configuration updates its description on the canvas
      Given the automation already has a "Status changed" trigger node with no status selected
      When the user opens the node configuration panel for that node
      And the user selects a status and clicks "Save"
      Then the node's description on the canvas should reflect the selected status

    Scenario: Removing a node asks for confirmation before deleting it
      Given the automation already has a "Status changed" trigger node
      When the user opens the node configuration panel for that node
      And the user clicks the "Remove node" button
      Then a confirmation dialog titled "Remove node" should appear
      When the user confirms the removal
      Then the node should no longer appear on the canvas

    Scenario: The node palette is hidden for a project member without workflows.write permission
      Given the user does not have the "workflows.write" project permission in "E2E_AUTOMATION_GRAPH_PROJECT"
      When the user opens the automation builder for "E2E_AUTOMATION_GRAPH"
      Then the node palette should be hidden

    Scenario: Switching to the Run history tab shows the run history panel
      When the user clicks the "Run history" tab
      Then the node palette should no longer be displayed
      And the run history panel should be displayed

    Scenario: A run history panel with no runs shows an empty state
      Given "E2E_AUTOMATION_GRAPH" has never run
      When the user clicks the "Run history" tab
      Then the run history panel should display the "No runs yet" empty state
