@projects @task-types
Feature: Task types management
  Project members with the appropriate permissions can configure the set of
  task types used to categorise work in a project.  Types are managed from
  Settings > Task Types on the project settings page.  Each type has a
  required name, an optional icon chosen from a preset library (Bug,
  Feature, Story, Epic, Task, Idea, Security, Chore, Branch, Critical,
  Important, Goal, Warning, Doc, Feedback, Package, Build, Test,
  Improvement, Refactor, Generic, Ticket, Checklist), an optional colour,
  and an optional description.  A default set of user-manageable types
  (Bug, Story, Task) is created with every new project.  Two system types
  (Epic, Subtask) are also seeded at project creation; these are read-only
  and cannot be created, edited, or deleted by users.

  @authenticated
  Rule: Viewing task types

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_TYPE_VIEW_PROJECT" exists
      And the user has navigated to the "E2E_TYPE_VIEW_PROJECT" Settings page

    Scenario: Task Types section is reachable from the settings sidebar
      When the user clicks "Task Types" in the settings sidebar
      Then the "Task Types" section heading should be visible
      And the section should display a description mentioning categorising tasks with custom types

    Scenario: Default user-manageable task types are pre-populated for a new project
      When the user clicks "Task Types" in the settings sidebar
      Then the task types table should contain a type named "Bug"
      And the task types table should contain a type named "Story"
      And the task types table should contain a type named "Task"

    Scenario: System task types Epic and Subtask are shown in a separate read-only section
      When the user clicks "Task Types" in the settings sidebar
      Then a "System types" read-only section should be visible below the user-manageable types table
      And the system types section should contain a type named "Epic"
      And the system types section should contain a type named "Subtask"
      And the system types section should display a note that these types cannot be modified

    Scenario: Task types table shows the expected columns
      When the user clicks "Task Types" in the settings sidebar
      Then the task types table should have columns "Icon", "Name", and "Description"

    Scenario: User-manageable type rows have Edit and Delete action buttons
      When the user clicks "Task Types" in the settings sidebar
      Then every user-manageable task type row should have an "Edit type" button
      And every user-manageable task type row should have a "Delete type" button

    Scenario: System type rows do not have Edit or Delete action buttons
      When the user clicks "Task Types" in the settings sidebar
      Then the "Epic" system type row should not have an "Edit type" button
      And the "Epic" system type row should not have a "Delete type" button
      And the "Subtask" system type row should not have an "Edit type" button
      And the "Subtask" system type row should not have a "Delete type" button

    Scenario: "New type" button is visible on the Task Types section
      When the user clicks "Task Types" in the settings sidebar
      Then the "New type" button should be visible

  @authenticated
  Rule: Creating a task type

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_TYPE_CREATE_PROJECT" exists
      And the user has navigated to the "E2E_TYPE_CREATE_PROJECT" Settings page
      And the user clicks "Task Types" in the settings sidebar

    Scenario: Opening the create-type dialog
      When the user clicks the "New type" button
      Then the "Create task type" dialog should open
      And the dialog should contain a required "Name" field
      And the dialog should contain an optional icon picker
      And the dialog should contain a colour picker
      And the dialog should contain an optional "Description" field

    Scenario: "Create type" button is disabled while the name field is empty
      When the user clicks the "New type" button
      Then the "Create type" button should be disabled

    Scenario: "Create type" button becomes enabled after typing a name
      When the user clicks the "New type" button
      And the user fills the type name with "E2E Epic"
      Then the "Create type" button should be enabled

    Scenario: Icon picker lists the available preset icons
      When the user clicks the "New type" button
      Then the icon picker should offer "Bug", "Feature", "Story", "Epic", "Task", "Idea",
        "Security", "Chore", "Branch", "Critical", "Important", "Goal", "Warning", "Doc",
        "Feedback", "Package", "Build", "Test", "Improvement", "Refactor", "Generic",
        "Ticket", and "Checklist" icons

    Scenario: Creating a type with only a name succeeds
      When the user clicks the "New type" button
      And the user fills the type name with "E2E Name Only"
      And the user clicks "Create type"
      Then the dialog should close
      And the task types table should contain a type named "E2E Name Only"

    Scenario: Creating a type with name and description succeeds
      When the user clicks the "New type" button
      And the user fills the type name with "E2E Described Type"
      And the user fills the type description with "Used for exploratory work"
      And the user clicks "Create type"
      Then the dialog should close
      And the task types table should contain a type named "E2E Described Type"
      And the row for "E2E Described Type" should display the description "Used for exploratory work"

    Scenario: Creating a type with a selected icon displays the icon in the table
      When the user clicks the "New type" button
      And the user fills the type name with "E2E Iconic Type"
      And the user selects the "Epic" icon
      And the user clicks "Create type"
      Then the dialog should close
      And the task types table should contain a type named "E2E Iconic Type"
      And the row for "E2E Iconic Type" should display an icon

    Scenario: Creating a type with a custom colour succeeds
      When the user clicks the "New type" button
      And the user fills the type name with "E2E Coloured Type"
      And the user enters a custom colour "#ef4444"
      And the user clicks "Create type"
      Then the dialog should close
      And the task types table should contain a type named "E2E Coloured Type"

    Scenario: Cancelling the create-type dialog discards changes
      When the user clicks the "New type" button
      And the user fills the type name with "E2E Should Not Exist Type"
      And the user clicks "Cancel"
      Then the dialog should close
      And the task types table should not contain a type named "E2E Should Not Exist Type"

    Scenario: Closing the create-type dialog with the Close button discards changes
      When the user clicks the "New type" button
      And the user fills the type name with "E2E Should Not Exist via X"
      And the user clicks the Close button on the dialog
      Then the dialog should close
      And the task types table should not contain a type named "E2E Should Not Exist via X"

  @authenticated
  Rule: Editing a task type

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_TYPE_EDIT_PROJECT" exists
      And a task type named "E2E Edit Me Type" exists in that project
      And the user has navigated to the "E2E_TYPE_EDIT_PROJECT" Settings page
      And the user clicks "Task Types" in the settings sidebar

    Scenario: Opening the edit-type dialog pre-fills existing values
      When the user clicks "Edit type" for the type named "E2E Edit Me Type"
      Then the "Edit task type" dialog should open
      And the "Name" field should be pre-filled with "E2E Edit Me Type"

    Scenario: Saving a new name updates the type in the table
      When the user clicks "Edit type" for the type named "E2E Edit Me Type"
      And the user clears the name and types "E2E Edited Type Name"
      And the user clicks "Save changes"
      Then the dialog should close
      And the task types table should contain a type named "E2E Edited Type Name"
      And the task types table should not contain a type named "E2E Edit Me Type"

    Scenario: Adding a description to an existing type updates it in the table
      When the user clicks "Edit type" for the type named "E2E Edit Me Type"
      And the user fills the type description with "Now has a description"
      And the user clicks "Save changes"
      Then the dialog should close
      And the row for "E2E Edit Me Type" should display the description "Now has a description"

    Scenario: Changing the icon updates it in the table row
      When the user clicks "Edit type" for the type named "E2E Edit Me Type"
      And the user selects the "Feature" icon
      And the user clicks "Save changes"
      Then the dialog should close
      And the row for "E2E Edit Me Type" should display an icon

    Scenario: Cancelling the edit-type dialog discards changes
      When the user clicks "Edit type" for the type named "E2E Edit Me Type"
      And the user clears the name and types "E2E Should Not Save Type"
      And the user clicks "Cancel"
      Then the dialog should close
      And the task types table should still contain a type named "E2E Edit Me Type"
      And the task types table should not contain a type named "E2E Should Not Save Type"

    Scenario: Clearing the name field disables the save button
      When the user clicks "Edit type" for the type named "E2E Edit Me Type"
      And the user clears the name field
      Then the "Save changes" button should be disabled

  @authenticated
  Rule: Deleting a task type

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_TYPE_DELETE_PROJECT" exists
      And a task type named "E2E Delete Me Type" exists in that project
      And the user has navigated to the "E2E_TYPE_DELETE_PROJECT" Settings page
      And the user clicks "Task Types" in the settings sidebar

    Scenario: Opening the delete-type dialog shows a confirmation message
      When the user clicks "Delete type" for the type named "E2E Delete Me Type"
      Then the "Delete task type" dialog should open
      And the dialog should identify the type being deleted by name
      And the dialog should warn that tasks using this type will lose their type assignment
      And the dialog should warn that the action cannot be undone

    Scenario: Confirming deletion removes the type from the table
      When the user clicks "Delete type" for the type named "E2E Delete Me Type"
      And the user confirms by clicking "Delete type" in the dialog
      Then the dialog should close
      And the task types table should not contain a type named "E2E Delete Me Type"

    Scenario: Cancelling the delete-type dialog keeps the type in the table
      When the user clicks "Delete type" for the type named "E2E Delete Me Type"
      And the user clicks "Cancel" in the delete confirmation dialog
      Then the dialog should close
      And the task types table should still contain a type named "E2E Delete Me Type"

    Scenario: Closing the delete-type dialog with the Close button keeps the type
      When the user clicks "Delete type" for the type named "E2E Delete Me Type"
      And the user clicks the Close button on the dialog
      Then the "Delete task type" dialog should close
      And the task types table should still contain a type named "E2E Delete Me Type"

    Scenario: The default type cannot be deleted
      Then the default type should show "Delete type" disabled with the reason "The default type can't be deleted. Make another type the default first."
      And the default type should still offer "Edit type"
      And the type named "E2E Delete Me Type" should offer "Delete type"
      And the API should refuse to delete the default type with the code "TASK_TYPE_IS_DEFAULT"

    Scenario: Making another type the default moves the protection to it
      When the user clicks "Set as default type" for the type named "E2E Delete Me Type"
      Then the type named "E2E Delete Me Type" should carry the "Default" mark
      And the type named "E2E Delete Me Type" should show "Delete type" disabled

  @authenticated
  Rule: System task types are protected from modification

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_SYSTEM_TYPE_PROJECT" exists
      And the user has navigated to the "E2E_SYSTEM_TYPE_PROJECT" Settings page
      And the user clicks "Task Types" in the settings sidebar

    Scenario: The "New type" button cannot create a type named "Epic" or "Subtask"
      When the user clicks the "New type" button
      And the user fills the type name with "Epic"
      And the user clicks "Create type"
      Then an error message should indicate that "Epic" is a reserved system type name

    Scenario: The "New type" button cannot create a type named "Subtask"
      When the user clicks the "New type" button
      And the user fills the type name with "Subtask"
      And the user clicks "Create type"
      Then an error message should indicate that "Subtask" is a reserved system type name
