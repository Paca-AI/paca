@projects @sprints @sprint-lifecycle
Feature: Sprint lifecycle management
  Sprints move through three states: planned (shown as "Draft") → active →
  completed.  A sprint is quick-created in the planned state with a
  system-generated name ("Sprint N", where N is the number of existing
  sprints plus one) and no dates — no creation dialog is shown.  Users with
  the "Manage Sprints" (sprints.write) project permission start a planned
  sprint from a "Start sprint" button, found in the sprint's column header on
  the product backlog Table view and in the header of the sprint's own page.
  Either opens a "Start sprint" modal where they confirm or edit the name,
  goal, start date (pre-filled with today) and end date before activating it.
  Starting from the backlog then navigates to the sprint page; starting from
  the sprint page stays on it.  More than one sprint may be active at once —
  the modal only shows a non-blocking warning naming the other active sprint.
  An active sprint is completed with the "Complete sprint" button in its page
  header, which opens a "Complete sprint" modal asking where the sprint's
  incomplete tasks should go (the product backlog or any other sprint that is
  not completed); confirming completes the sprint and returns the user to the
  product backlog.  Any sprint can be deleted through the "Edit sprint" modal
  and a "Delete sprint?" confirmation; its tasks return to the product
  backlog.  Draft sprints are listed under a collapsible "Draft Sprints"
  section of the project sidebar, active sprints directly under "Product
  Backlog", and completed sprints under a "Completed Sprints" section that is
  collapsed by default.

  @authenticated
  Rule: Creating a sprint (quick create — no modal)

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_SPRINT_CREATE" exists with no sprints
      And the user has the "Manage Sprints" project permission in "E2E_SPRINT_CREATE"
      And the user has navigated to the product backlog of "E2E_SPRINT_CREATE"

    Scenario: Clicking "New sprint" in the page header creates a draft sprint with a default name
      When the user clicks "New sprint" in the product backlog page header
      Then a sprint named "Sprint 1" should appear as a column with a "Draft" badge
      And "Sprint 1" should be listed under "Draft Sprints" in the project sidebar
      And no creation dialog should appear

    Scenario: Sequential quick-creates produce incrementally numbered names
      When the user clicks "New sprint" in the product backlog page header
      And the user clicks "New sprint" in the product backlog page header again
      Then sprints named "Sprint 1" and "Sprint 2" should both appear as columns

    Scenario: The "New sprint" button is not shown without "Manage Sprints" permission
      Given a member of "E2E_SPRINT_CREATE" who only has the "View Sprints" project permission
      When that member opens the product backlog of "E2E_SPRINT_CREATE"
      Then the "New sprint" button should not be visible

  @authenticated
  Rule: Starting a sprint

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_SPRINT_START" exists
      And the project has a draft sprint named "E2E_SPRINT_START_ME"
      And the user has the "Manage Sprints" project permission in "E2E_SPRINT_START"
      And the user has navigated to the product backlog of "E2E_SPRINT_START"

    Scenario: A draft sprint column header shows a "Start sprint" button
      Then the column header for "E2E_SPRINT_START_ME" should contain a "Start sprint" button

    Scenario: Clicking "Start sprint" opens the Start sprint modal with the sprint's fields
      When the user clicks "Start sprint" in the "E2E_SPRINT_START_ME" column header
      Then a "Start sprint" modal should open
      And the "Name" field should contain "E2E_SPRINT_START_ME"
      And the "Goal" field should be empty
      And the "Start date" field should contain today's date
      And the "End date" field should be empty

    Scenario: Starting with the default values activates the sprint and opens its page
      When the user clicks "Start sprint" in the "E2E_SPRINT_START_ME" column header
      And the user clicks "Start sprint" in the modal
      Then the page should navigate to the "E2E_SPRINT_START_ME" sprint page
      And the sprint page should show an "Active" badge and a "Complete sprint" button

    Scenario: Goal and dates entered in the modal are saved
      When the user clicks "Start sprint" in the "E2E_SPRINT_START_ME" column header
      And the user fills the goal with "Deliver authentication"
      And the user sets the start date to "2026-04-14" and the end date to "2026-04-27"
      And the user clicks "Start sprint" in the modal
      Then the sprint should be active with goal "Deliver authentication"
      And the sprint should have start date "2026-04-14" and end date "2026-04-27"
      And the sprint page description should show the goal

    Scenario: Renaming the sprint in the Start sprint modal updates its name
      When the user clicks "Start sprint" in the "E2E_SPRINT_START_ME" column header
      And the user changes the name to "E2E_SPRINT_RENAMED"
      And the user clicks "Start sprint" in the modal
      Then the sprint page heading should read "E2E_SPRINT_RENAMED"

    Scenario: An end date before the start date is rejected in the modal
      When the user clicks "Start sprint" in the "E2E_SPRINT_START_ME" column header
      And the user sets the start date to "2026-04-27" and the end date to "2026-04-14"
      Then the modal should show "Due date can't be before the start date."
      And the modal's "Start sprint" button should be disabled

    Scenario: Cancelling the modal leaves the sprint as a draft
      When the user clicks "Start sprint" in the "E2E_SPRINT_START_ME" column header
      And the user fills the goal with "Should not be saved"
      And the user clicks "Cancel" in the modal
      Then the modal should close
      And the sprint should still be a draft with no goal

    Scenario: An active sprint column header has no "Start sprint" button
      Given the project has an active sprint named "E2E_SPRINT_START_ACTIVE"
      When the user opens the product backlog of "E2E_SPRINT_START"
      Then the column header for "E2E_SPRINT_START_ACTIVE" should not contain a "Start sprint" button

    Scenario: A draft sprint can also be started from its own page
      When the user opens the "E2E_SPRINT_START_ME" sprint page
      Then the page header should contain a "Start sprint" button
      When the user clicks "Start sprint" in the page header
      And the user clicks "Start sprint" in the modal
      Then the user should stay on the "E2E_SPRINT_START_ME" sprint page
      And the page should show an "Active" badge and a "Complete sprint" button

    Scenario: "Start sprint" is not offered without "Manage Sprints" permission
      Given a member of "E2E_SPRINT_START" who only has the "View Sprints" project permission
      When that member opens the product backlog of "E2E_SPRINT_START"
      Then the column header for "E2E_SPRINT_START_ME" should not contain a "Start sprint" button

  @authenticated
  Rule: Completing a sprint

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_SPRINT_COMPLETE" exists
      And the project has an active sprint named "E2E_SPRINT_COMPLETE_ME"
      And the project has a draft sprint named "E2E_SPRINT_NEXT"
      And the active sprint has incomplete tasks "E2E_SPRINT_TASK_1" and "E2E_SPRINT_TASK_2"
      And the active sprint has a done task "E2E_SPRINT_DONE_TASK"
      And the user has the "Manage Sprints" project permission in "E2E_SPRINT_COMPLETE"
      And the user has opened the "E2E_SPRINT_COMPLETE_ME" sprint page

    Scenario: The sprint page header shows the sprint name, status and a "Complete sprint" button
      Then the page heading should read "E2E_SPRINT_COMPLETE_ME"
      And the page should show an "Active" badge and a "Complete sprint" button

    Scenario: Clicking "Complete sprint" opens a modal counting the incomplete tasks
      When the user clicks "Complete sprint" in the page header
      Then a "Complete sprint" modal should open
      And it should say "2 incomplete tasks will be moved to:"

    Scenario: The modal offers the product backlog and every other unfinished sprint as destinations
      When the user clicks "Complete sprint" in the page header
      Then the modal should offer a "Product Backlog" option, selected by default
      And the modal should offer an "E2E_SPRINT_NEXT" option with a "Draft" badge
      And the modal should not offer "E2E_SPRINT_COMPLETE_ME" itself

    Scenario: Completing with another sprint as destination moves the incomplete tasks there
      When the user clicks "Complete sprint" in the page header
      And the user selects "E2E_SPRINT_NEXT" as the destination
      And the user clicks "Complete sprint" in the modal
      Then the user should be taken to the product backlog
      And "E2E_SPRINT_COMPLETE_ME" should have status "completed"
      And "E2E_SPRINT_TASK_1" and "E2E_SPRINT_TASK_2" should belong to "E2E_SPRINT_NEXT"

    Scenario: Completing with the product backlog as destination unassigns the incomplete tasks
      When the user clicks "Complete sprint" in the page header
      And the user clicks "Complete sprint" in the modal
      Then "E2E_SPRINT_COMPLETE_ME" should have status "completed"
      And "E2E_SPRINT_TASK_1" and "E2E_SPRINT_TASK_2" should belong to no sprint

    Scenario: Done tasks stay on the completed sprint
      When the user clicks "Complete sprint" in the page header
      And the user clicks "Complete sprint" in the modal
      Then "E2E_SPRINT_DONE_TASK" should still belong to "E2E_SPRINT_COMPLETE_ME"

    Scenario: A completed sprint moves to the collapsed "Completed Sprints" sidebar section
      When the user clicks "Complete sprint" in the page header
      And the user clicks "Complete sprint" in the modal
      Then the project sidebar should show a "Completed Sprints" section with a count of 1
      When the user expands "Completed Sprints"
      Then "E2E_SPRINT_COMPLETE_ME" should be listed under it

    Scenario: Cancelling the modal leaves the sprint active
      When the user clicks "Complete sprint" in the page header
      And the user clicks "Cancel" in the modal
      Then the modal should close
      And "E2E_SPRINT_COMPLETE_ME" should still have status "active"
      And "E2E_SPRINT_TASK_1" should still belong to "E2E_SPRINT_COMPLETE_ME"

    Scenario: A sprint with no incomplete tasks shows a confirmation message and no destinations
      Given the active sprint has no incomplete tasks
      When the user clicks "Complete sprint" in the page header
      Then the modal should say "No incomplete tasks remain in this sprint."
      And the modal should not offer any destination options
      And the modal should still contain "Complete sprint" and "Cancel" buttons

    Scenario: "Complete sprint" is not offered without "Manage Sprints" permission
      Given a member of "E2E_SPRINT_COMPLETE" who only has the "View Sprints" project permission
      When that member opens the "E2E_SPRINT_COMPLETE_ME" sprint page
      Then the page header should not contain a "Complete sprint" button

  @authenticated
  Rule: Sprint lifecycle state constraints

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_SPRINT_STATE" exists
      And the user has the "Manage Sprints" project permission in "E2E_SPRINT_STATE"

    Scenario: Multiple sprints can be active at the same time
      Given the project has an active sprint named "E2E_SPRINT_ACTIVE_A"
      And the project has a draft sprint named "E2E_SPRINT_DRAFT_B"
      When the user starts "E2E_SPRINT_DRAFT_B" from the product backlog
      Then the Start sprint modal should warn that "E2E_SPRINT_ACTIVE_A" is already active
      And both "E2E_SPRINT_ACTIVE_A" and "E2E_SPRINT_DRAFT_B" should have status "active"

    Scenario: A draft sprint can be deleted and its tasks return to the product backlog
      Given the project has a draft sprint named "E2E_SPRINT_DELETE_DRAFT" containing task "E2E_SPRINT_ORPHAN"
      When the user opens the "E2E_SPRINT_DELETE_DRAFT" sprint page
      And the user clicks "Edit sprint"
      And the user clicks "Delete sprint" in the Edit sprint modal
      Then a "Delete sprint?" confirmation naming "E2E_SPRINT_DELETE_DRAFT" should appear
      When the user confirms with "Delete sprint"
      Then the user should be taken to the product backlog
      And "E2E_SPRINT_DELETE_DRAFT" should no longer exist
      And "E2E_SPRINT_ORPHAN" should belong to no sprint

    Scenario: Cancelling the delete confirmation keeps the sprint
      Given the project has a draft sprint named "E2E_SPRINT_KEEP_DRAFT"
      When the user opens the "E2E_SPRINT_KEEP_DRAFT" sprint page
      And the user clicks "Edit sprint"
      And the user clicks "Delete sprint" in the Edit sprint modal
      And the user cancels the "Delete sprint?" confirmation
      Then "E2E_SPRINT_KEEP_DRAFT" should still exist

    Scenario: An active sprint can be deleted too
      Given the project has an active sprint named "E2E_SPRINT_DELETE_ACTIVE"
      When the user deletes "E2E_SPRINT_DELETE_ACTIVE" through the Edit sprint modal
      Then "E2E_SPRINT_DELETE_ACTIVE" should no longer exist

    Scenario: A completed sprint can be opened but cannot be started or completed again
      Given the project has a completed sprint named "E2E_SPRINT_DONE"
      When the user opens the "E2E_SPRINT_DONE" sprint page
      Then the page should show a "Completed" badge
      And the page header should not contain a "Start sprint" button
      And the page header should not contain a "Complete sprint" button

    Scenario: Completing an already-completed sprint through the API is rejected
      Given the project has a completed sprint named "E2E_SPRINT_DONE_API"
      When the sprint is completed again through the API
      Then the request should be rejected
