@projects @roles
Feature: Project role management
  A project's Settings page has a "Roles" section where members with the
  project.roles.write permission define what each role may do inside that
  project (routes/_authenticated/projects/$projectId/settings/, component
  RolesSettings). Every new project is seeded with three project-scoped
  roles — "Admin", "Editor" and "Viewer". Each role is a set of permission
  grants that are edited through the role form dialog (ProjectRoleFormDialog):
  one switch per permission, grouped by area. When every permission of an area
  is enabled the grant is stored as that area's wildcard (for example
  "tasks.*"). The seeded "Admin" role is stored as the bare wildcard "*", which
  the form shows as a "Full access" badge — that role automatically includes
  every permission, including ones added in the future, until a toggle is
  changed. Viewing the section needs project.roles.read; creating, editing and
  deleting roles need project.roles.write. A role that is still assigned to a
  member cannot be deleted.

  @authenticated
  Rule: Roles list

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_ROLES_LIST" exists
      And the user has navigated to the Settings page of "E2E_ROLES_LIST"

    Scenario: The Roles section lists the default project roles
      When the user opens the "Roles" section
      Then the "Project Roles" heading should be visible
      And the roles table should have the columns "Name", "Permissions" and "Created"
      And the roles table should list the roles "Admin", "Editor" and "Viewer"

    Scenario: The seeded Admin role shows the wildcard grant
      When the user opens the "Roles" section
      Then the "Admin" row should show the permission badge "*"

    Scenario: A role with no permissions shows a placeholder
      Given a project role named "E2E_ROLES_EMPTY" with no permissions exists in "E2E_ROLES_LIST"
      When the user opens the "Roles" section
      Then the "E2E_ROLES_EMPTY" row should show "No permissions assigned"

    Scenario: The permission badges of a role list its granted permissions
      Given a project role named "E2E_ROLES_BADGES" granting "tasks.read" and "docs.read" exists in "E2E_ROLES_LIST"
      When the user opens the "Roles" section
      Then the "E2E_ROLES_BADGES" row should show the permission badges "tasks.read" and "docs.read"

  @authenticated
  Rule: Creating a role

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_ROLES_CREATE" exists
      And the user has opened the "Roles" section of the Settings page of "E2E_ROLES_CREATE"

    Scenario: The New role button opens an empty role form
      When the user clicks "New role"
      Then a dialog titled "New Role" should open
      And the "Role Name" field should be empty
      And the "Create role" button should be disabled

    Scenario: The Create role button is enabled once a name is entered
      When the user clicks "New role"
      And the user types "E2E_ROLES_NEW" into the "Role Name" field
      Then the "Create role" button should be enabled

    Scenario: Enabling permission switches updates the enabled count
      When the user clicks "New role"
      And the user turns on the "View Tasks" and "View Documents" switches
      Then the permissions header should show "2 enabled"

    Scenario: Creating a role with individual permissions adds it to the list
      When the user clicks "New role"
      And the user types "E2E_ROLES_READER" into the "Role Name" field
      And the user turns on the "View Tasks" and "View Documents" switches
      And the user clicks "Create role"
      Then the dialog should close
      And the roles table should list "E2E_ROLES_READER"
      And the "E2E_ROLES_READER" row should show the permission badges "tasks.read" and "docs.read"

    Scenario: Enabling every permission of an area is stored as the area wildcard
      When the user clicks "New role"
      And the user types "E2E_ROLES_TASKS" into the "Role Name" field
      And the user turns on the "View Tasks" and "Edit Tasks" switches
      And the user clicks "Create role"
      Then the "E2E_ROLES_TASKS" row should show the permission badge "tasks.*"

    Scenario: A role name that already exists is rejected
      When the user clicks "New role"
      And the user types "Admin" into the "Role Name" field
      And the user clicks "Create role"
      Then the error "A role with this name already exists." should be shown
      And the dialog should stay open

    Scenario: Cancelling the form discards the new role
      When the user clicks "New role"
      And the user types "E2E_ROLES_DISCARDED" into the "Role Name" field
      And the user clicks "Cancel"
      Then the dialog should close
      And the roles table should not list "E2E_ROLES_DISCARDED"

  @authenticated
  Rule: Editing a role

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_ROLES_EDIT" exists
      And a project role named "E2E_ROLES_EDITABLE" granting "tasks.read" exists in "E2E_ROLES_EDIT"
      And the user has opened the "Roles" section of the Settings page of "E2E_ROLES_EDIT"

    Scenario: The edit form is pre-filled with the role's name and permissions
      When the user clicks "Edit role" on the "E2E_ROLES_EDITABLE" row
      Then a dialog titled "Edit Role" should open
      And the "Role Name" field should contain "E2E_ROLES_EDITABLE"
      And the "View Tasks" switch should be on
      And the "Edit Tasks" switch should be off
      And the permissions header should show "1 enabled"

    Scenario: Renaming a role and adding a permission updates the list
      When the user clicks "Edit role" on the "E2E_ROLES_EDITABLE" row
      And the user changes the "Role Name" field to "E2E_ROLES_RENAMED"
      And the user turns on the "View Documents" switch
      And the user clicks "Save changes"
      Then the dialog should close
      And the roles table should list "E2E_ROLES_RENAMED"
      And the roles table should not list "E2E_ROLES_EDITABLE"
      And the "E2E_ROLES_RENAMED" row should show the permission badges "tasks.read" and "docs.read"

    Scenario: Turning a permission off removes it from the role
      When the user clicks "Edit role" on the "E2E_ROLES_EDITABLE" row
      And the user turns off the "View Tasks" switch
      And the user clicks "Save changes"
      Then the "E2E_ROLES_EDITABLE" row should show "No permissions assigned"

    Scenario: Cancelling the edit form leaves the role unchanged
      When the user clicks "Edit role" on the "E2E_ROLES_EDITABLE" row
      And the user changes the "Role Name" field to "E2E_ROLES_NOT_SAVED"
      And the user clicks "Cancel"
      Then the roles table should list "E2E_ROLES_EDITABLE"
      And the roles table should not list "E2E_ROLES_NOT_SAVED"

  @authenticated
  Rule: Full access roles

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_ROLES_FULL" exists
      And a project role named "E2E_ROLES_EVERYTHING" stored as the bare wildcard "*" exists in "E2E_ROLES_FULL"
      And the user has opened the "Roles" section of the Settings page of "E2E_ROLES_FULL"

    Scenario: A role stored as the wildcard shows the Full access badge
      When the user clicks "Edit role" on the "E2E_ROLES_EVERYTHING" row
      Then the permissions header should show the "Full access" badge
      And the explanation "This role automatically includes every permission, including ones added in the future" should be visible
      And every permission switch should be on

    Scenario: The seeded Admin role is a Full access role
      When the user clicks "Edit role" on the "Admin" row
      Then the permissions header should show the "Full access" badge

    Scenario: A role with an enumerated permission set does not show the Full access badge
      Given a project role named "E2E_ROLES_PARTIAL" granting "tasks.read" exists in "E2E_ROLES_FULL"
      When the user clicks "Edit role" on the "E2E_ROLES_PARTIAL" row
      Then the "Full access" badge should not be visible

    Scenario: Changing any switch converts a Full access role to a fixed permission set
      When the user clicks "Edit role" on the "E2E_ROLES_EVERYTHING" row
      And the user turns off the "Manage Roles" switch
      Then the "Full access" badge should not be visible
      And the permissions header should show an enabled count
      When the user clicks "Save changes"
      Then the "E2E_ROLES_EVERYTHING" row should no longer show the permission badge "*"
      And the stored permissions of "E2E_ROLES_EVERYTHING" should not contain "*"

    Scenario: Saving an untouched Full access role keeps the wildcard
      When the user clicks "Edit role" on the "E2E_ROLES_EVERYTHING" row
      And the user clicks "Save changes"
      Then the stored permissions of "E2E_ROLES_EVERYTHING" should still be the bare wildcard "*"

  @authenticated
  Rule: Deleting a role

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_ROLES_DELETE" exists
      And a project role named "E2E_ROLES_DISPOSABLE" granting "tasks.read" exists in "E2E_ROLES_DELETE"
      And the user has opened the "Roles" section of the Settings page of "E2E_ROLES_DELETE"

    Scenario: Deleting a role asks for confirmation naming the role
      When the user clicks "Delete role" on the "E2E_ROLES_DISPOSABLE" row
      Then a dialog titled "Delete role" should open
      And the dialog should ask "Are you sure you want to delete E2E_ROLES_DISPOSABLE?"

    Scenario: Confirming the deletion removes the role
      When the user clicks "Delete role" on the "E2E_ROLES_DISPOSABLE" row
      And the user confirms with "Delete role"
      Then the dialog should close
      And the roles table should not list "E2E_ROLES_DISPOSABLE"

    Scenario: Cancelling the deletion keeps the role
      When the user clicks "Delete role" on the "E2E_ROLES_DISPOSABLE" row
      And the user clicks "Cancel"
      Then the dialog should close
      And the roles table should list "E2E_ROLES_DISPOSABLE"

    Scenario: A role that is still assigned to a member cannot be deleted
      Given a member "E2E_ROLES_ASSIGNED" is assigned the role "E2E_ROLES_IN_USE" in "E2E_ROLES_DELETE"
      When the user reloads the Roles section
      And the user clicks "Delete role" on the "E2E_ROLES_IN_USE" row
      And the user confirms with "Delete role"
      Then the error "This role cannot be deleted because it is still assigned to one or more members." should be shown
      And the roles table should still list "E2E_ROLES_IN_USE" after the dialog is closed

  @authenticated
  Rule: Access to role management is permission gated

    Background:
      Given a project named "E2E_ROLES_GATING" exists

    Scenario: A member without project.roles.read sees the no-permission state
      Given the user is a member of "E2E_ROLES_GATING" with only the "tasks.read" permission
      When the user opens the "Roles" section of the Settings page of "E2E_ROLES_GATING"
      Then the message "You don't have permission to view roles" should be displayed
      And the roles table should not be displayed

    Scenario: A member with only project.roles.read can view roles but not change them
      Given the user is a member of "E2E_ROLES_GATING" with only the "project.roles.read" permission
      When the user opens the "Roles" section of the Settings page of "E2E_ROLES_GATING"
      Then the roles table should list the roles "Admin", "Editor" and "Viewer"
      And the "New role" button should not be visible
      And the "Admin" row should not offer "Edit role" or "Delete role"

    Scenario: A member with project.roles.write can create, edit and delete roles
      Given the user is a member of "E2E_ROLES_GATING" with the "project.roles.read" and "project.roles.write" permissions
      When the user opens the "Roles" section of the Settings page of "E2E_ROLES_GATING"
      Then the "New role" button should be visible
      And the "Admin" row should offer "Edit role" and "Delete role"
