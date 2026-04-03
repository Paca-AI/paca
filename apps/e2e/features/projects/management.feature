@projects @project-management
Feature: Project management
  Projects are the primary workspace unit in the workspace. Visibility,
  creation, editing, deletion, and member or role management are each gated
  by distinct global or project-scoped permissions. The project list lives
  on the home page; all per-project settings are accessed through the
  project's own Settings page.

  @authenticated
  Rule: Listing projects on the home page

    Background:
      Given the user already has a stored authenticated session
      And the user navigates to the home page

    Scenario: User with "Read All Projects" global permission sees every project
      Given the user has the "Read All Projects" global permission
      Then the projects section should display all projects in the workspace

    Scenario: User without "Read All Projects" sees only projects they are a member of
      Given the user does not have the "Read All Projects" global permission
      And the user is a member of project "E2E_ALPHA"
      And a project named "E2E_BETA" exists that the user is not a member of
      Then the projects section should include "E2E_ALPHA"
      And the projects section should not include "E2E_BETA"

    Scenario: User with no membership and no "Read All Projects" sees an empty state
      Given the user does not have the "Read All Projects" global permission
      And the user is not a member of any project
      Then the projects section should show an empty state

    Scenario: User with "Read All Projects" sees projects they are not personally a member of
      Given the user has the "Read All Projects" global permission
      And a project named "E2E_HIDDEN_PROJECT" exists that the user is not a member of
      Then the projects section should include "E2E_HIDDEN_PROJECT"

    Scenario: Project cards show name, description, and creation date
      Given at least one project named "E2E_LISTED_PROJECT" exists
      Then the card for "E2E_LISTED_PROJECT" should show the project name
      And the card should show the project description or "No description"
      And the card should show the project creation date

    Scenario: Home page stats reflect the total and active project count
      Then the stats bar should show the total number of projects
      And the stats bar should show the count of active projects

  @authenticated
  Rule: Creating a project

    Background:
      Given the user already has a stored authenticated session
      And the user navigates to the home page

    Scenario: "New Project" button is visible for users with "Create Projects" global permission
      Given the user has the "Create Projects" global permission
      Then the "New Project" button should be visible

    Scenario: "New Project" button is not visible without "Create Projects" global permission
      Given the user does not have the "Create Projects" global permission
      Then the "New Project" button should not be visible

    Scenario: New project dialog has required name field and optional description field
      Given the user has the "Create Projects" global permission
      When the user clicks the "New Project" button
      Then the "New project" dialog should open
      And the dialog should contain a required "Project name" field
      And the dialog should contain an optional "Description" field

    Scenario: "Create project" button is disabled while the project name is empty
      Given the user has the "Create Projects" global permission
      When the user clicks the "New Project" button
      Then the "Create project" button should be disabled

    Scenario: Creating a project with only a name succeeds
      Given the user has the "Create Projects" global permission
      When the user clicks the "New Project" button
      And the user fills the project name with "E2E_NAME_ONLY_PROJECT"
      And the user clicks "Create project"
      Then the dialog should close
      And the project "E2E_NAME_ONLY_PROJECT" should appear in the projects section

    Scenario: Creating a project with name and description succeeds
      Given the user has the "Create Projects" global permission
      When the user clicks the "New Project" button
      And the user fills the project name with "E2E_DESCRIBED_PROJECT"
      And the user fills the description with "A test project with a description"
      And the user clicks "Create project"
      Then the dialog should close
      And the project "E2E_DESCRIBED_PROJECT" should appear in the projects section
      And the card for "E2E_DESCRIBED_PROJECT" should show "A test project with a description"

    Scenario: Cancelling the create project dialog discards changes
      Given the user has the "Create Projects" global permission
      When the user clicks the "New Project" button
      And the user fills the project name with "E2E_SHOULD_NOT_EXIST"
      And the user clicks "Cancel"
      Then the project "E2E_SHOULD_NOT_EXIST" should not appear in the projects section

    Scenario: Creating a project with a duplicate name shows a validation error
      Given the user has the "Create Projects" global permission
      And a project named "E2E_DUPLICATE_PROJECT" already exists
      When the user clicks the "New Project" button
      And the user fills the project name with "E2E_DUPLICATE_PROJECT"
      And the user clicks "Create project"
      Then a validation error should indicate the project name is already taken
      And the dialog should remain open

  @authenticated
  Rule: Navigating into a project

    Scenario: Clicking a project card opens the project dashboard
      Given the user has access to project "E2E_NAV_PROJECT"
      When the user clicks the card for "E2E_NAV_PROJECT"
      Then the user should be on the "E2E_NAV_PROJECT" dashboard page

    Scenario: Project sidebar shows Dashboard, Integrations, Docs, Team, and Settings links
      Given the user is inside project "E2E_NAV_PROJECT"
      Then the sidebar should contain a "Dashboard" link
      And the sidebar should contain an "Integrations" link
      And the sidebar should contain a "Docs" link
      And the sidebar should contain a "Team" link
      And the sidebar should contain a "Settings" link

    Scenario: User with "Read All Projects" can navigate to any project by direct URL
      Given the user has the "Read All Projects" global permission
      And a project named "E2E_ANY_PROJECT" exists
      When the user navigates directly to the "E2E_ANY_PROJECT" project dashboard URL
      Then the project dashboard should be visible

    Scenario: User without "Read All Projects" is denied access to a project they are not in
      Given the user does not have the "Read All Projects" global permission
      And the user is not a member of project "E2E_RESTRICTED_PROJECT"
      When the user navigates directly to the "E2E_RESTRICTED_PROJECT" project dashboard URL
      Then the user should see an access-denied message

  @authenticated
  Rule: Editing project settings (Settings > General)

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_SETTINGS_PROJECT" exists
      And the user has navigated to the "E2E_SETTINGS_PROJECT" Settings page

    Scenario: General tab shows editable project name and description fields
      When the user clicks "General" in the settings sidebar
      Then a "Project name" field pre-filled with "E2E_SETTINGS_PROJECT" should be visible
      And a "Description" field should be visible

    Scenario: "Save changes" button is disabled until a change is made
      When the user clicks "General" in the settings sidebar
      Then the "Save changes" button should be disabled

    Scenario: Saving a new project name updates the project
      When the user clicks "General" in the settings sidebar
      And the user clears the project name and types "E2E_RENAMED_SETTINGS_PROJECT"
      And the user clicks "Save changes"
      Then the project name should be updated to "E2E_RENAMED_SETTINGS_PROJECT"

    Scenario: Saving a new description updates the project
      When the user clicks "General" in the settings sidebar
      And the user fills the description with "Updated description"
      And the user clicks "Save changes"
      Then the project description should reflect "Updated description"

    Scenario: Clearing the project name disables "Save changes"
      When the user clicks "General" in the settings sidebar
      And the user clears the project name
      Then the "Save changes" button should be disabled

    Scenario: Saving a name already used by another project shows a validation error
      Given a project named "E2E_EXISTING_PROJECT" already exists
      When the user clicks "General" in the settings sidebar
      And the user clears the project name and types "E2E_EXISTING_PROJECT"
      And the user clicks "Save changes"
      Then a validation error should indicate the project name is already taken

    Scenario: Settings General is accessible to users with "Write Projects" global permission
      Given the user has the "Write Projects" global permission
      Then the "General" settings tab and its fields should be visible and editable

    Scenario: Settings General is accessible to users with project-scoped "Edit Project" permission
      Given the user does not have the "Write Projects" global permission
      And the user has the "Edit Project" project role in "E2E_SETTINGS_PROJECT"
      Then the "General" settings tab and its fields should be visible and editable

    Scenario: A user without any update permission cannot edit project settings
      Given the user does not have the "Write Projects" global permission
      And the user does not have the "Edit Project" project role in "E2E_SETTINGS_PROJECT"
      Then the "General" settings section should be read-only or inaccessible

  @authenticated
  Rule: Deleting a project (Settings > Danger Zone)

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_DELETE_PROJECT" exists
      And the user has navigated to the "E2E_DELETE_PROJECT" Settings page
      And the user clicks "Danger Zone" in the settings sidebar

    Scenario: Danger Zone displays the "Delete project" button with a warning
      Then a "Delete project" button should be visible
      And the section should warn that the action is permanent and cannot be undone

    Scenario: Delete project dialog warns about permanent data loss
      When the user clicks the "Delete project" button
      Then the "Delete project" dialog should open
      And the dialog should warn that all members, roles, and integrations will be lost

    Scenario: "Delete permanently" button is disabled until the project name is typed
      When the user clicks the "Delete project" button
      Then the "Delete permanently" button should be disabled
      And the dialog should instruct the user to type "E2E_DELETE_PROJECT" to confirm

    Scenario: Typing an incorrect name keeps "Delete permanently" disabled
      When the user clicks the "Delete project" button
      And the user types "wrong-name" in the confirmation field
      Then the "Delete permanently" button should remain disabled

    Scenario: Typing the exact project name enables "Delete permanently"
      When the user clicks the "Delete project" button
      And the user types "E2E_DELETE_PROJECT" in the confirmation field
      Then the "Delete permanently" button should become enabled

    Scenario: Confirming deletion removes the project
      When the user clicks the "Delete project" button
      And the user types "E2E_DELETE_PROJECT" in the confirmation field
      And the user clicks "Delete permanently"
      Then the user should be redirected away from the project
      And the project "E2E_DELETE_PROJECT" should no longer appear on the home page

    Scenario: Cancelling the delete dialog preserves the project
      When the user clicks the "Delete project" button
      And the user clicks "Cancel"
      Then the project "E2E_DELETE_PROJECT" should still appear on the home page

    Scenario: "Delete project" button is accessible with "Delete Projects" global permission
      Given the user has the "Delete Projects" global permission
      Then the "Delete project" button should be visible in Danger Zone

    Scenario: "Delete project" button is accessible with project-scoped "Delete Project" permission
      Given the user does not have the "Delete Projects" global permission
      And the user has the "Delete Project" project role in "E2E_DELETE_PROJECT"
      Then the "Delete project" button should be visible in Danger Zone

    Scenario: User without delete permission cannot access project deletion
      Given the user does not have the "Delete Projects" global permission
      And the user does not have the "Delete Project" project role in "E2E_DELETE_PROJECT"
      Then the "Delete project" button should not be visible in Danger Zone

  @authenticated
  Rule: Managing team members (Team page)

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_TEAM_PROJECT" exists

    Scenario: Team page shows heading and project subtitle
      When the user navigates to the "E2E_TEAM_PROJECT" Team page
      Then the page heading "Team" should be visible
      And the subtitle should include "E2E_TEAM_PROJECT" and "Manage project members and roles"

    Scenario: "Add Member" button is visible for users with "Write Project Members" global permission
      Given the user has the "Write Project Members" global permission
      When the user navigates to the "E2E_TEAM_PROJECT" Team page
      Then the "Add Member" button should be visible

    Scenario: "Add Member" button is not visible without member management permission
      Given the user does not have the "Write Project Members" global permission
      And the user does not have the "Manage Members" project role in "E2E_TEAM_PROJECT"
      When the user navigates to the "E2E_TEAM_PROJECT" Team page
      Then the "Add Member" button should not be visible

    Scenario: Add Member dialog contains a user search field and a role picker
      Given the user has the "Write Project Members" global permission
      When the user navigates to the "E2E_TEAM_PROJECT" Team page
      And the user clicks "Add Member"
      Then the "Add member" dialog should open
      And the dialog should contain a "Search by name or username" field
      And the dialog should contain a role picker

    Scenario: "Add member" button in the dialog is disabled when no user is selected
      Given the user has the "Write Project Members" global permission
      When the user navigates to the "E2E_TEAM_PROJECT" Team page
      And the user clicks "Add Member"
      Then the "Add member" dialog button should be disabled

    Scenario: Dialog shows "No users available to add" when all users are already members
      Given the user has the "Write Project Members" global permission
      And all system users are already members of "E2E_TEAM_PROJECT"
      When the user navigates to the "E2E_TEAM_PROJECT" Team page
      And the user clicks "Add Member"
      Then the user search should indicate "No users available to add."

    Scenario: Adding a member successfully adds them to the team list
      Given the user has the "Write Project Members" global permission
      And a user named "E2E_INVITED_USER" exists
      And "E2E_INVITED_USER" is not yet a member of "E2E_TEAM_PROJECT"
      When the user navigates to the "E2E_TEAM_PROJECT" Team page
      And the user clicks "Add Member"
      And the user searches for and selects "E2E_INVITED_USER"
      And the user clicks "Add member"
      Then "E2E_INVITED_USER" should appear in the team list

    Scenario: Member row shows name, username, Change role button, and a remove action
      When the user navigates to the "E2E_TEAM_PROJECT" Team page
      Then each member row should show the member's name and username
      And each row should have a "Change role" button
      And each row should have a menu button that contains a "Remove member" option

    Scenario: Removing a member removes them from the team list
      Given the user has the "Write Project Members" global permission
      And "E2E_REMOVABLE_USER" is a member of "E2E_TEAM_PROJECT"
      When the user navigates to the "E2E_TEAM_PROJECT" Team page
      And the user opens the menu for "E2E_REMOVABLE_USER" and clicks "Remove member"
      Then "E2E_REMOVABLE_USER" should no longer appear in the team list

    Scenario: User with "Write Project Members" global permission can manage members of any project
      Given the user has the "Write Project Members" global permission
      And a project named "E2E_FOREIGN_PROJECT" exists that the user is not a member of
      When the user navigates to the "E2E_FOREIGN_PROJECT" Team page
      Then the "Add Member" button should be visible

  @authenticated
  Rule: Managing project roles (Settings > Roles)

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_ROLES_PROJECT" exists
      And the user has navigated to the "E2E_ROLES_PROJECT" Settings page
      And the user clicks "Roles" in the settings sidebar

    Scenario: Project Roles heading and subtitle are visible
      Then the "Project Roles" heading should be visible
      And the subtitle should read "Manage roles and permissions for members of this project."

    Scenario: Statistics bar shows the role count and permission grant total
      Then the statistics bar should show the total number of project roles
      And the statistics bar should show the total permission grants across all project roles

    Scenario: Roles table displays expected columns
      Then the project roles table should have columns "Name", "Permissions", and "Created"

    Scenario: Default roles Admin, Editor, and Viewer are pre-seeded
      Then the role "Admin" should appear in the project roles table
      And the role "Editor" should appear in the project roles table
      And the role "Viewer" should appear in the project roles table

    Scenario: Each role row shows Edit role and Delete role action buttons
      Then each role row should have an "Edit role" button
      And each role row should have a "Delete role" button

  @authenticated
  Rule: Creating a project role

    Background:
      Given the user already has a stored authenticated session
      And the user is on the "E2E_ROLES_PROJECT" project roles settings page

    Scenario: New Role dialog title and description are correct
      When the user clicks the "New role" button
      Then the role form dialog should open
      And the dialog title should be "New Role"
      And the dialog description should mention creating a project role with permissions

    Scenario: Role Name field is empty and Create role button is disabled by default
      When the user clicks the "New role" button
      Then the "Role Name" field should be empty
      And the "Create role" button should be disabled

    Scenario: Permission form shows five groups: Project, Members, Roles, Tasks, Sprints
      When the user clicks the "New role" button
      Then the permission section should display a "Project" group
      And the permission section should display a "Members" group
      And the permission section should display a "Roles" group
      And the permission section should display a "Tasks" group
      And the permission section should display a "Sprints" group

    Scenario: Each project permission shows expected label and description
      When the user clicks the "New role" button
      Then the "Read Project" permission should show description "View project details and settings"
      And the "Edit Project" permission should show description "Update project name, description, and settings"
      And the "Delete Project" permission should show description "Permanently delete this project"
      And the "View Members" permission should show description "List and view project members"
      And the "Manage Members" permission should show description "Add, remove, and reassign project members"
      And the "View Roles" permission should show description "List and view project role definitions"
      And the "Manage Roles" permission should show description "Create, edit, and delete project roles"
      And the "View Tasks" permission should show description "Browse and read tasks in the project"
      And the "Edit Tasks" permission should show description "Create, update, and move tasks"
      And the "View Sprints" permission should show description "Browse sprint boards and backlogs"
      And the "Manage Sprints" permission should show description "Create, update, and close sprints"

    Scenario: All permission switches are off by default
      When the user clicks the "New role" button
      Then the "Read Project" permission switch should be off
      And the "Edit Project" permission switch should be off
      And the "Delete Project" permission switch should be off
      And the "View Members" permission switch should be off
      And the "Manage Members" permission switch should be off
      And the "View Roles" permission switch should be off
      And the "Manage Roles" permission switch should be off
      And the "View Tasks" permission switch should be off
      And the "Edit Tasks" permission switch should be off
      And the "View Sprints" permission switch should be off
      And the "Manage Sprints" permission switch should be off

    Scenario: Creating a role with a name and selected permissions
      When the user clicks the "New role" button
      And the user fills the role name with "E2E_PROJECT_MANAGER"
      And the user enables the "Edit Project" permission
      And the user enables the "Manage Members" permission
      And the user clicks "Create role"
      Then the dialog should close
      And the role "E2E_PROJECT_MANAGER" should appear in the project roles table
      And the statistics bar should reflect the updated role count

    Scenario: Creating a role without any permissions is allowed
      When the user clicks the "New role" button
      And the user fills the role name with "E2E_EMPTY_ROLE"
      And the user clicks "Create role"
      Then the role "E2E_EMPTY_ROLE" should appear in the project roles table
      And the role should show zero active permissions

    Scenario: Cancelling the dialog discards changes
      When the user clicks the "New role" button
      And the user fills the role name with "E2E_SHOULD_NOT_EXIST"
      And the user clicks "Cancel"
      Then the role "E2E_SHOULD_NOT_EXIST" should not appear in the project roles table

    Scenario: Closing and reopening the dialog resets all permission switches
      When the user clicks the "New role" button
      And the user enables the "Delete Project" permission
      And the user closes the dialog
      And the user clicks the "New role" button again
      Then all permission switches should be in their default state

    Scenario: Permissions count in the table matches the granted permissions
      When the user clicks the "New role" button
      And the user fills the role name with "E2E_COUNT_ROLE"
      And the user enables the "Edit Project" permission
      And the user enables the "Delete Project" permission
      And the user clicks "Create role"
      Then the role "E2E_COUNT_ROLE" should show 2 active permissions
      And the statistics bar should reflect the added permission grants

    Scenario: Toggling a permission on then off leaves it disabled
      When the user clicks the "New role" button
      And the user enables the "Delete Project" permission
      And the user disables the "Delete Project" permission
      Then the "Delete Project" permission switch should be off

  @authenticated
  Rule: Editing a project role

    Background:
      Given the user already has a stored authenticated session
      And the user is on the "E2E_ROLES_PROJECT" project roles settings page
      And a project role named "E2E_EDITABLE_ROLE" exists in the project

    Scenario: Opening the edit dialog pre-populates current data
      When the user clicks the "Edit role" button for "E2E_EDITABLE_ROLE"
      Then the role form dialog should open
      And the dialog title should be "Edit Role"
      And the "Role Name" field should be pre-filled with "E2E_EDITABLE_ROLE"
      And the permission switches should reflect the role's current permissions

    Scenario: Saving an updated name and permissions
      When the user clicks the "Edit role" button for "E2E_EDITABLE_ROLE"
      And the user clears the role name and types "E2E_RENAMED_ROLE"
      And the user toggles the "Delete Project" permission
      And the user clicks "Save changes"
      Then the dialog should close
      And the role "E2E_RENAMED_ROLE" should appear in the project roles table

    Scenario: Cancelling the edit dialog discards all changes
      When the user clicks the "Edit role" button for "E2E_EDITABLE_ROLE"
      And the user clears the role name and types "E2E_UNSAVED_CHANGE"
      And the user clicks "Cancel"
      Then the role "E2E_EDITABLE_ROLE" should still appear in the project roles table
      And the role "E2E_UNSAVED_CHANGE" should not appear in the project roles table

    Scenario: Enabling additional permissions on an existing project role
      Given "E2E_EDITABLE_ROLE" exists with only "Edit Project" permission
      When the user clicks the "Edit role" button for "E2E_EDITABLE_ROLE"
      And the user enables the "Delete Project" permission
      And the user enables the "Manage Members" permission
      And the user clicks "Save changes"
      Then the dialog should close
      And the role "E2E_EDITABLE_ROLE" should show 3 active permissions

    Scenario: Edit dialog pre-populates the correct permission switches
      Given "E2E_EDITABLE_ROLE" exists with "Delete Project" and "Manage Sprints" permissions
      When the user clicks the "Edit role" button for "E2E_EDITABLE_ROLE"
      Then the "Delete Project" permission switch should be on
      And the "Manage Sprints" permission switch should be on
      And the "Edit Project" permission switch should be off
      And the "View Members" permission switch should be off

    Scenario: Removing all permissions from an existing project role
      Given "E2E_EDITABLE_ROLE" exists with "Edit Project" and "View Tasks" permissions
      When the user clicks the "Edit role" button for "E2E_EDITABLE_ROLE"
      And the user disables the "Edit Project" permission
      And the user disables the "View Tasks" permission
      And the user clicks "Save changes"
      Then the dialog should close
      And the role "E2E_EDITABLE_ROLE" should show zero active permissions

    Scenario: Toggling a permission off during edit persists after save
      Given "E2E_EDITABLE_ROLE" exists with "Manage Members" permission
      When the user clicks the "Edit role" button for "E2E_EDITABLE_ROLE"
      And the user disables the "Manage Members" permission
      And the user clicks "Save changes"
      Then the dialog should close
      When the user clicks the "Edit role" button for "E2E_EDITABLE_ROLE"
      Then the "Manage Members" permission switch should be off

  @authenticated
  Rule: Deleting a project role

    Background:
      Given the user already has a stored authenticated session
      And the user is on the "E2E_ROLES_PROJECT" project roles settings page
      And a project role named "E2E_DELETABLE_ROLE" exists in the project

    Scenario: Confirming deletion removes the project role
      When the user clicks the "Delete role" button for "E2E_DELETABLE_ROLE"
      Then a delete confirmation dialog should open
      And the dialog should display the name "E2E_DELETABLE_ROLE"
      When the user confirms deletion
      Then the role "E2E_DELETABLE_ROLE" should no longer appear in the project roles table
      And the statistics bar should reflect the updated role and permission counts

    Scenario: Cancelling the delete dialog preserves the project role
      When the user clicks the "Delete role" button for "E2E_DELETABLE_ROLE"
      And the user cancels deletion
      Then the role "E2E_DELETABLE_ROLE" should still appear in the project roles table

  @authenticated
  Rule: Access control for project roles settings

    Scenario: Users with "Manage Roles" project permission see the New role button and edit/delete actions
      Given the user has the "Manage Roles" project role in "E2E_ROLES_PROJECT"
      When the user navigates to the "E2E_ROLES_PROJECT" project roles settings page
      Then the "New role" button should be visible
      And "Edit role" and "Delete role" buttons should be visible on each role row

    Scenario: Users without role management permission see roles but cannot modify them
      Given the user does not have role management permission in "E2E_ROLES_PROJECT"
      When the user navigates to the "E2E_ROLES_PROJECT" project roles settings page
      Then the "New role" button should not be visible
      And no "Edit role" or "Delete role" buttons should be visible

    Scenario: User without project access cannot reach the project roles settings page
      Given the user does not have the "Read All Projects" global permission
      And the user is not a member of project "E2E_ROLES_PROJECT"
      When the user navigates directly to the "E2E_ROLES_PROJECT" project roles settings URL
      Then the user should see an access-denied message
