@projects @task-statuses
Feature: Task statuses management
  Project members with the appropriate permissions can configure the set of
  workflow statuses that tasks move through inside a project.  Statuses are
  managed from Settings > Task Statuses on the project settings page.  Each
  status has a name, a category (Backlog, Refinement, Ready, To Do,
  In Progress, or Done), and a colour.  A default set of statuses
  (Backlog, Todo, In Progress, Done) is created with every new project and
  can be edited or deleted just like any user-created status.

  @authenticated
  Rule: Viewing task statuses

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_STATUS_VIEW_PROJECT" exists
      And the user has navigated to the "E2E_STATUS_VIEW_PROJECT" Settings page

    Scenario: Task Statuses section is reachable from the settings sidebar
      When the user clicks "Task Statuses" in the settings sidebar
      Then the "Task Statuses" section heading should be visible
      And the section should display a description mentioning workflow statuses

    Scenario: Default statuses are pre-populated for a new project
      When the user clicks "Task Statuses" in the settings sidebar
      Then the statuses table should contain a status named "Backlog" with category "Backlog"
      And the statuses table should contain a status named "Todo" with category "To Do"
      And the statuses table should contain a status named "In Progress" with category "In Progress"
      And the statuses table should contain a status named "Done" with category "Done"

    Scenario: Statuses table shows the expected columns
      When the user clicks "Task Statuses" in the settings sidebar
      Then the statuses table should have columns "#", "Name", and "Category"

    Scenario: Each status row has Edit and Delete action buttons
      When the user clicks "Task Statuses" in the settings sidebar
      Then every status row should have an "Edit status" button
      And every status row should have a "Delete status" button

    Scenario: "New status" button is visible on the Task Statuses section
      When the user clicks "Task Statuses" in the settings sidebar
      Then the "New status" button should be visible

  @authenticated
  Rule: Creating a task status

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_STATUS_CREATE_PROJECT" exists
      And the user has navigated to the "E2E_STATUS_CREATE_PROJECT" Settings page
      And the user clicks "Task Statuses" in the settings sidebar

    Scenario: Opening the create-status dialog
      When the user clicks the "New status" button
      Then the "Create status" dialog should open
      And the dialog should contain a required "Name" field
      And the dialog should contain a "Category" dropdown
      And the dialog should contain a colour picker

    Scenario: "Create status" button is disabled while the name field is empty
      When the user clicks the "New status" button
      Then the "Create status" button should be disabled

    Scenario: "Create status" button becomes enabled after typing a name
      When the user clicks the "New status" button
      And the user fills the status name with "E2E In Review"
      Then the "Create status" button should be enabled

    Scenario: Category dropdown lists all available categories
      When the user clicks the "New status" button
      And the user opens the Category dropdown
      Then the dropdown should list "Backlog"
      And the dropdown should list "Refinement"
      And the dropdown should list "Ready"
      And the dropdown should list "To Do"
      And the dropdown should list "In Progress"
      And the dropdown should list "Done"

    Scenario: Default category in the Create status dialog is "To Do"
      When the user clicks the "New status" button
      Then the "Category" dropdown should display "To Do" as the selected value

    Scenario: Creating a status with only a name succeeds
      When the user clicks the "New status" button
      And the user fills the status name with "E2E In Review"
      And the user clicks "Create status"
      Then the dialog should close
      And the statuses table should contain a status named "E2E In Review"

    Scenario: Creating a status with name and category succeeds
      When the user clicks the "New status" button
      And the user fills the status name with "E2E Refinement"
      And the user selects category "Refinement"
      And the user clicks "Create status"
      Then the dialog should close
      And the statuses table should contain a status named "E2E Refinement" with category "Refinement"

    Scenario: Creating a status with a custom colour succeeds
      When the user clicks the "New status" button
      And the user fills the status name with "E2E Custom Colour"
      And the user enters a custom colour "#22c55e"
      And the user clicks "Create status"
      Then the dialog should close
      And the statuses table should contain a status named "E2E Custom Colour"

    Scenario: Cancelling the create-status dialog discards changes
      When the user clicks the "New status" button
      And the user fills the status name with "E2E Should Not Exist"
      And the user clicks "Cancel"
      Then the dialog should close
      And the statuses table should not contain a status named "E2E Should Not Exist"

    Scenario: Closing the create-status dialog with the Close button discards changes
      When the user clicks the "New status" button
      And the user fills the status name with "E2E Should Not Exist via X"
      And the user clicks the Close button on the dialog
      Then the dialog should close
      And the statuses table should not contain a status named "E2E Should Not Exist via X"

  @authenticated
  Rule: Editing a task status

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_STATUS_EDIT_PROJECT" exists
      And a status named "E2E Edit Me" exists in that project with category "To Do"
      And the user has navigated to the "E2E_STATUS_EDIT_PROJECT" Settings page
      And the user clicks "Task Statuses" in the settings sidebar

    Scenario: Opening the edit-status dialog pre-fills existing values
      When the user clicks "Edit status" for the status named "E2E Edit Me"
      Then the "Edit status" dialog should open
      And the "Name" field should be pre-filled with "E2E Edit Me"
      And the "Category" dropdown should display the current category

    Scenario: Saving a new name updates the status in the table
      When the user clicks "Edit status" for the status named "E2E Edit Me"
      And the user clears the name and types "E2E Edited Name"
      And the user clicks "Save changes"
      Then the dialog should close
      And the statuses table should contain a status named "E2E Edited Name"
      And the statuses table should not contain a status named "E2E Edit Me"

    Scenario: Changing the category updates the status in the table
      When the user clicks "Edit status" for the status named "E2E Edit Me"
      And the user selects category "In Progress"
      And the user clicks "Save changes"
      Then the dialog should close
      And the statuses table should contain a status named "E2E Edit Me" with category "In Progress"

    Scenario: Cancelling the edit-status dialog discards changes
      When the user clicks "Edit status" for the status named "E2E Edit Me"
      And the user clears the name and types "E2E Should Not Save"
      And the user clicks "Cancel"
      Then the dialog should close
      And the statuses table should still contain a status named "E2E Edit Me"
      And the statuses table should not contain a status named "E2E Should Not Save"

    Scenario: Clearing the name field and saving shows a validation error
      When the user clicks "Edit status" for the status named "E2E Edit Me"
      And the user clears the name field
      Then the "Save changes" button should be disabled

  @authenticated
  Rule: Deleting a task status

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_STATUS_DELETE_PROJECT" exists
      And a status named "E2E Delete Me" exists in that project
      And the user has navigated to the "E2E_STATUS_DELETE_PROJECT" Settings page
      And the user clicks "Task Statuses" in the settings sidebar

    Scenario: Opening the delete-status dialog shows a confirmation message
      When the user clicks "Delete status" for the status named "E2E Delete Me"
      Then the "Delete status" dialog should open
      And the dialog should identify the status being deleted by name
      And the dialog should warn that tasks using this status will lose their status
      And the dialog should warn that the action cannot be undone

    Scenario: Confirming deletion removes the status from the table
      When the user clicks "Delete status" for the status named "E2E Delete Me"
      And the user confirms by clicking "Delete status" in the dialog
      Then the dialog should close
      And the statuses table should not contain a status named "E2E Delete Me"

    Scenario: Cancelling the delete-status dialog keeps the status in the table
      When the user clicks "Delete status" for the status named "E2E Delete Me"
      And the user clicks "Cancel" in the delete confirmation dialog
      Then the dialog should close
      And the statuses table should still contain a status named "E2E Delete Me"

    Scenario: Closing the delete-status dialog with the Close button keeps the status
      When the user clicks "Delete status" for the status named "E2E Delete Me"
      And the user clicks the Close button on the dialog
      Then the statuses table should still contain a status named "E2E Delete Me"

    Scenario: The default status cannot be deleted
      Then the default status should show "Delete status" disabled with the reason "The default status can't be deleted. Make another status the default first."
      And the default status should still offer "Edit status"
      And the status named "E2E Delete Me" should offer "Delete status"
      And the API should refuse to delete the default status with the code "TASK_STATUS_IS_DEFAULT"

    Scenario: Making another status the default lets the old default be deleted
      When the user clicks "Set as default status" for the status named "E2E Delete Me"
      Then the status named "E2E Delete Me" should carry the "Default" mark
      And the status named "E2E Delete Me" should show "Delete status" disabled
      And the status named "Backlog" should offer "Delete status"
