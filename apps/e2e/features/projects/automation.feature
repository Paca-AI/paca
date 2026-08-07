@projects @automation
Feature: Workflow automation
  Automations are per-project workflow graphs: a set of trigger, condition,
  and action nodes wired together with edges, executed by the automation
  engine whenever a trigger fires. The Automation list page
  (routes/_authenticated/projects/$projectId/automation/) shows every
  automation as a card with a status badge (draft, active, or archived),
  and can toggle a project-wide dependency map showing which automations
  watch which tasks. Creating an automation immediately navigates into its
  graph builder (routes/.../automation/$automationId.tsx), which starts
  empty and in "draft" status. A draft automation can be freely edited and
  can be activated; an active automation can be archived (terminal) or
  reverted back to draft to resume editing; an archived automation's graph
  becomes read-only. All write actions (create, delete, node/edge editing,
  rename, activate, archive, revert) are gated by the workflows.write
  project permission.

  @authenticated
  Rule: Automation list page — loading, empty state, and dependency map

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_AUTOMATION_PROJECT" exists
      And the user has navigated to the "E2E_AUTOMATION_PROJECT" project

    Scenario: The Automation page shows loading skeletons while automations are being fetched
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      Then the Automation page should briefly display loading skeleton cards

    Scenario: A project with no automations shows an empty state
      Given "E2E_AUTOMATION_PROJECT" has no automations
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      Then the Automation page should display an empty state with a "Create your first automation" action

    Scenario: The "New automation" button is visible with workflows.write permission
      Given the user has the "workflows.write" project permission in "E2E_AUTOMATION_PROJECT"
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      Then the "New automation" button should be visible

    Scenario: The "New automation" button is hidden without workflows.write permission
      Given the user does not have the "workflows.write" project permission in "E2E_AUTOMATION_PROJECT"
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      Then the "New automation" button should not be visible

    Scenario: The delete button on a card is hidden without workflows.write permission
      Given the user does not have the "workflows.write" project permission in "E2E_AUTOMATION_PROJECT"
      And "E2E_AUTOMATION_PROJECT" has a draft automation named "E2E_AUTOMATION_READONLY"
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      Then the card for "E2E_AUTOMATION_READONLY" should not show a delete button

    Scenario: An automation card shows its name, description, status badge, and last-updated time
      Given "E2E_AUTOMATION_PROJECT" has a draft automation named "E2E_AUTOMATION_CARD" with description "Notify on overdue tasks"
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      Then the card for "E2E_AUTOMATION_CARD" should display the description "Notify on overdue tasks"
      And the card for "E2E_AUTOMATION_CARD" should display the "Draft" status badge

    Scenario: An automation card with no description shows a placeholder
      Given "E2E_AUTOMATION_PROJECT" has a draft automation named "E2E_AUTOMATION_NO_DESC" with no description
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      Then the card for "E2E_AUTOMATION_NO_DESC" should display the "No description" placeholder

    Scenario: Toggling the dependency map shows watched-task counts per automation
      Given "E2E_AUTOMATION_PROJECT" has an active automation named "E2E_AUTOMATION_DEP" watching at least one task
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      And the user clicks the "Dependency map" button
      Then the dependency map panel should display "E2E_AUTOMATION_DEP" with its watched task count

    Scenario: The dependency map shows an empty state when no automation watches any task
      Given "E2E_AUTOMATION_PROJECT" has no automations that watch tasks
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      And the user clicks the "Dependency map" button
      Then the dependency map panel should display its own empty state

    Scenario: Clicking the dependency map button again hides the panel
      When the user navigates to the Automation page for "E2E_AUTOMATION_PROJECT"
      And the user clicks the "Dependency map" button
      And the user clicks the "Dependency map" button
      Then the dependency map panel should be hidden

    Scenario: Clicking an automation card navigates into its graph builder
      Given "E2E_AUTOMATION_PROJECT" has a draft automation named "E2E_AUTOMATION_OPEN"
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
      When the user clicks the "New automation" button
      Then the create automation dialog should open
      And the "Create" button should be disabled
      When the user fills the automation name with "E2E_AUTOMATION_NEW"
      Then the "Create" button should be enabled

    Scenario: Creating an automation navigates straight into its (empty, draft) graph builder
      When the user clicks the "New automation" button
      And the user fills the automation name with "E2E_AUTOMATION_CREATED"
      And the user fills the automation description with "Runs when a bug is filed"
      And the user clicks "Create"
      Then the create automation dialog should close
      And the page URL should be the automation builder URL for "E2E_AUTOMATION_CREATED"
      And the automation builder should show the "Draft" status badge
      And the automation graph canvas should be empty

    Scenario: Cancelling the create dialog discards the in-progress automation
      When the user clicks the "New automation" button
      And the user fills the automation name with "E2E_AUTOMATION_CANCELLED"
      And the user clicks "Cancel"
      Then the create automation dialog should close
      And the Automation page should not list "E2E_AUTOMATION_CANCELLED"

    Scenario: Deleting an automation asks for confirmation before removing it
      Given "E2E_AUTOMATION_CRUD_PROJECT" has a draft automation named "E2E_AUTOMATION_TO_DELETE"
      When the user navigates to the Automation page for "E2E_AUTOMATION_CRUD_PROJECT"
      And the user clicks the delete button on the card for "E2E_AUTOMATION_TO_DELETE"
      Then a delete confirmation dialog should appear naming "E2E_AUTOMATION_TO_DELETE"
      When the user confirms the deletion
      Then "E2E_AUTOMATION_TO_DELETE" should no longer appear on the Automation page

    Scenario: Cancelling the delete confirmation keeps the automation
      Given "E2E_AUTOMATION_CRUD_PROJECT" has a draft automation named "E2E_AUTOMATION_KEEP"
      When the user navigates to the Automation page for "E2E_AUTOMATION_CRUD_PROJECT"
      And the user clicks the delete button on the card for "E2E_AUTOMATION_KEEP"
      And the user cancels the delete confirmation
      Then "E2E_AUTOMATION_KEEP" should still appear on the Automation page

  @authenticated
  Rule: Automation status lifecycle — draft, active, archived, and back to draft

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_AUTOMATION_LIFECYCLE_PROJECT" exists
      And the user has the "workflows.write" project permission in "E2E_AUTOMATION_LIFECYCLE_PROJECT"

    Scenario: A draft automation shows an "Activate" button
      Given "E2E_AUTOMATION_LIFECYCLE_PROJECT" has a draft automation named "E2E_AUTOMATION_DRAFT"
      When the user opens the automation builder for "E2E_AUTOMATION_DRAFT"
      Then the "Activate" button should be visible
      And neither the "Archive" nor "Deactivate" button should be visible

    Scenario: Activating a draft automation flips its status to active
      Given "E2E_AUTOMATION_LIFECYCLE_PROJECT" has a draft automation named "E2E_AUTOMATION_TO_ACTIVATE"
      When the user opens the automation builder for "E2E_AUTOMATION_TO_ACTIVATE"
      And the user clicks the "Activate" button
      Then the automation builder should show the "Active" status badge
      And the "Activate" button should no longer be visible

    Scenario: An active automation shows "Archive" and "Deactivate" buttons instead of "Activate"
      Given "E2E_AUTOMATION_LIFECYCLE_PROJECT" has an active automation named "E2E_AUTOMATION_ACTIVE"
      When the user opens the automation builder for "E2E_AUTOMATION_ACTIVE"
      Then the "Archive" button should be visible
      And the "Deactivate" button should be visible
      And the "Activate" button should not be visible

    Scenario: Archiving an active automation makes its graph read-only
      Given "E2E_AUTOMATION_LIFECYCLE_PROJECT" has an active automation named "E2E_AUTOMATION_TO_ARCHIVE"
      When the user opens the automation builder for "E2E_AUTOMATION_TO_ARCHIVE"
      And the user clicks the "Archive" button
      Then the automation builder should show the "Archived" status badge
      And a read-only notice should be displayed
      And the node palette should be hidden

    Scenario: Reverting (deactivating) an active automation sends it back to draft
      Given "E2E_AUTOMATION_LIFECYCLE_PROJECT" has an active automation named "E2E_AUTOMATION_TO_REVERT"
      When the user opens the automation builder for "E2E_AUTOMATION_TO_REVERT"
      And the user clicks the "Deactivate" button
      Then the automation builder should show the "Draft" status badge
      And the "Activate" button should be visible again

    Scenario: An archived automation shows no lifecycle action buttons
      Given "E2E_AUTOMATION_LIFECYCLE_PROJECT" has an archived automation named "E2E_AUTOMATION_ARCHIVED"
      When the user opens the automation builder for "E2E_AUTOMATION_ARCHIVED"
      Then neither the "Activate", "Archive", nor "Deactivate" button should be visible

    Scenario: Renaming an automation is disabled once it is archived
      Given "E2E_AUTOMATION_LIFECYCLE_PROJECT" has an archived automation named "E2E_AUTOMATION_RENAME_LOCKED"
      When the user opens the automation builder for "E2E_AUTOMATION_RENAME_LOCKED"
      Then the rename (pencil) icon next to the automation name should not be visible

    Scenario: Renaming a draft automation updates its name in the header
      Given "E2E_AUTOMATION_LIFECYCLE_PROJECT" has a draft automation named "E2E_AUTOMATION_OLD_NAME"
      When the user opens the automation builder for "E2E_AUTOMATION_OLD_NAME"
      And the user clicks the rename (pencil) icon next to the automation name
      And the user types "E2E_AUTOMATION_NEW_NAME" into the rename field
      And the user confirms the rename
      Then the automation builder header should display "E2E_AUTOMATION_NEW_NAME"

  @authenticated
  Rule: Building the automation graph — nodes, palette, and run history

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_AUTOMATION_GRAPH_PROJECT" exists
      And the user has the "workflows.write" project permission in "E2E_AUTOMATION_GRAPH_PROJECT"
      And "E2E_AUTOMATION_GRAPH_PROJECT" has a draft automation named "E2E_AUTOMATION_GRAPH"
      And the user has opened the automation builder for "E2E_AUTOMATION_GRAPH"

    Scenario: The graph tab is shown by default with a node palette for a draft automation
      Then the "Graph" tab should be active
      And the node palette should offer trigger, condition, and action node types

    Scenario: Adding a trigger node places it on the canvas and selects it
      When the user adds a "Status changed" trigger node from the palette
      Then a new trigger node should appear on the canvas
      And the node configuration panel should open for the new node

    Scenario: The node palette is hidden for a project member without workflows.write permission
      Given the user does not have the "workflows.write" project permission in "E2E_AUTOMATION_GRAPH_PROJECT"
      When the user opens the automation builder for "E2E_AUTOMATION_GRAPH"
      Then the node palette should be hidden

    Scenario: Switching to the Runs tab shows the run history panel
      When the user clicks the "Runs" tab
      Then the "Runs" tab should be active
      And the run history panel should be displayed

    Scenario: A run history panel with no runs shows an empty state
      Given "E2E_AUTOMATION_GRAPH" has never run
      When the user clicks the "Runs" tab
      Then the run history panel should display an empty state
