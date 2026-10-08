@projects @roles
Feature: Project role management
  A project's Settings page has a "Roles" section where members with the
  roles:write permission define what each role may do inside that
  project (routes/_authenticated/projects/$projectId/settings/, component
  RolesSettings). Every new project is seeded with four project-scoped
  roles - "Admin", "Editor" and "Viewer"; "Admin" is built in: it can be
  edited but not deleted. The list also shows workspace roles that are already
  attached to someone inside the project (with a lock, as they are shared and
  cannot be changed here), not every workspace role. A role is an IAM policy document (Allow and Deny statements over
  actions and resources), edited through the role form dialog
  (ProjectRoleFormDialog) either as one switch per permission, grouped by
  area (Simple), or as the policy JSON (Advanced), which can say more than the
  switches can, such as a Deny on one agent. When every permission of an area
  is enabled the grant is stored as that area's wildcard (for example
  "tasks:*"). A role stored as the bare wildcard "*" is shown by the form as a
  "Full access" badge - it automatically includes every permission, including
  ones added in the future, until a switch is changed. Viewing the section
  needs roles:read; creating, editing and deleting roles need roles:write.
  A member can hold several roles. Deleting a role that is still assigned
  removes it from its members.

  @authenticated
  Rule: Roles list

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_ROLES_LIST" exists
      And the user has navigated to the Settings page of "E2E_ROLES_LIST"

    Scenario: The Roles section lists the default project roles
      When the user opens the "Roles" section
      Then the "Project Roles" heading should be visible
      And the roles table should have the columns "Name", "Description" and "Created", and no "Permissions" column
      And the roles table should list the roles "Admin", "Editor" and "Viewer"

    Scenario: The seeded Admin role is a full-access role
      When the user opens the "Roles" section
      Then the "Admin" role should be stored with the wildcard grant "*"

    Scenario: A role without a description shows a placeholder
      Given a project role named "E2E_ROLES_EMPTY" with no permissions exists in "E2E_ROLES_LIST"
      When the user opens the "Roles" section
      Then the "E2E_ROLES_EMPTY" row should show "No description"

    Scenario: The description of a role is listed instead of its permissions
      Given a project role named "E2E_ROLES_DESCRIBED" described as "Reads tasks and documents" and granting "tasks:read" and "docs:read" exists in "E2E_ROLES_LIST"
      When the user opens the "Roles" section
      Then the "E2E_ROLES_DESCRIBED" row should show "Reads tasks and documents"
      And the "E2E_ROLES_DESCRIBED" row should not show any permission badge

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
      When the user clicks "Create role"
      Then the message "Enter a role name of up to 100 characters." should be shown
      And the dialog should stay open

    Scenario: The New role form starts in the Simple view with every switch off
      When the user clicks "New role"
      Then the "Simple" mode should be selected
      And every permission switch should be off
      And the "Create role" button should be enabled

    Scenario: A role is created with a description that the list shows
      When the user clicks "New role"
      And the user fills the role name with "E2E_ROLES_DESCRIBED_NEW"
      And the user fills the description with "Can read tasks, nothing else"
      And the user turns on the "View Tasks" switch
      And the user clicks "Create role"
      Then the "E2E_ROLES_DESCRIBED_NEW" row should show "Can read tasks, nothing else"

    Scenario: The permission list can be searched and a group switched on at once
      When the user clicks "New role"
      And the user searches the permissions for "View Tasks"
      Then only the "View Tasks" switch should be listed
      When the user searches the permissions for "no such permission"
      Then no switch is listed and a "Clear search" button is offered
      When the user clicks "Select all in Tasks"
      Then every Tasks switch should be on

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
      And the "E2E_ROLES_READER" role should be stored with the permissions "tasks:read" and "docs:read"

    Scenario: Enabling every permission of an area is stored as the area wildcard
      When the user clicks "New role"
      And the user types "E2E_ROLES_TASKS" into the "Role Name" field
      And the user turns on the "View Tasks" and "Edit Tasks" switches
      And the user clicks "Create role"
      Then the "E2E_ROLES_TASKS" role should be stored with the permissions "tasks:*"

    Scenario: A role can be written as a policy in the Advanced view, Deny included
      When the user clicks "New role"
      And the user types "E2E_ROLES_POLICY" into the "Role Name" field
      And the user switches to "Advanced (JSON)"
      And the user enters a policy that allows "tasks:read" and "tasks:write" and denies "tasks:write"
      And the user clicks "Create role"
      Then the "E2E_ROLES_POLICY" role should be stored with the permissions "tasks:read" and "tasks:write"
      And the stored policy of "E2E_ROLES_POLICY" should be an Allow statement followed by a Deny statement

    Scenario: A project role cannot name resources outside its project
      When a project role with the resource "project/*" is created through the API
      Then the request should be rejected with 422 ROLE_POLICY_INVALID at "statements[0].resources[0]"
      And a workspace role with the resource "project/<projectId>/*" can be created

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
      And a project role named "E2E_ROLES_EDITABLE" granting "tasks:read" exists in "E2E_ROLES_EDIT"
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
      And the "E2E_ROLES_RENAMED" role should be stored with the permissions "tasks:read" and "docs:read"

    Scenario: The description can be edited and cleared
      When the user clicks "Edit role" on the "E2E_ROLES_EDITABLE" row
      And the user changes the "Description" field to "Edited description"
      And the user clicks "Save changes"
      Then the "E2E_ROLES_EDITABLE" row should show "Edited description"
      When the user clicks "Edit role" on the "E2E_ROLES_EDITABLE" row
      And the user clears the "Description" field
      And the user clicks "Save changes"
      Then the "E2E_ROLES_EDITABLE" row should show "No description"

    Scenario: Turning a permission off removes it from the role
      When the user clicks "Edit role" on the "E2E_ROLES_EDITABLE" row
      And the user turns off the "View Tasks" switch
      And the user clicks "Save changes"
      Then the "E2E_ROLES_EDITABLE" role should be stored with no permissions

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

    Scenario: The built-in Admin role is full access and can be edited but not deleted
      Then the "Admin" role should be stored with the permissions "*"
      And the "Admin" row should offer "Edit role" but not "Delete role"

    Scenario: A role with an enumerated permission set does not show the Full access badge
      Given a project role named "E2E_ROLES_PARTIAL" granting "tasks:read" exists in "E2E_ROLES_FULL"
      When the user clicks "Edit role" on the "E2E_ROLES_PARTIAL" row
      Then the "Full access" badge should not be visible

    Scenario: Changing any switch converts a Full access role to a fixed permission set
      When the user clicks "Edit role" on the "E2E_ROLES_EVERYTHING" row
      And the user turns off the "Manage Roles" switch
      Then the "Full access" badge should not be visible
      And the permissions header should show an enabled count
      When the user clicks "Save changes"
      Then the "E2E_ROLES_EVERYTHING" role should no longer be stored with the wildcard grant "*"
      And the stored actions of "E2E_ROLES_EVERYTHING" should not contain "*" or "roles:write"

    Scenario: Saving an untouched Full access role keeps the wildcard
      When the user clicks "Edit role" on the "E2E_ROLES_EVERYTHING" row
      And the user clicks "Save changes"
      Then the stored actions of "E2E_ROLES_EVERYTHING" should still be the bare wildcard "*"

  @authenticated
  Rule: Deleting a role

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_ROLES_DELETE" exists
      And a project role named "E2E_ROLES_DISPOSABLE" granting "tasks:read" exists in "E2E_ROLES_DELETE"
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

    Scenario: Deleting a role that is assigned to a member takes it away from them
      Given a member "E2E_ROLES_ASSIGNED" is assigned the role "E2E_ROLES_IN_USE" in "E2E_ROLES_DELETE"
      When the user reloads the Roles section
      And the user clicks "Delete role" on the "E2E_ROLES_IN_USE" row
      Then the dialog should warn that members assigned this role will lose their access
      When the user confirms with "Delete role"
      Then the roles table should not list "E2E_ROLES_IN_USE"
      And "E2E_ROLES_ASSIGNED" should still be a member of "E2E_ROLES_DELETE" without that role

  @authenticated
  Rule: Access to role management is permission gated

    Background:
      Given a project named "E2E_ROLES_GATING" exists

    Scenario: A member without roles:read sees the no-permission state
      Given the user is a member of "E2E_ROLES_GATING" with only the "tasks:read" permission
      When the user opens the "Roles" section of the Settings page of "E2E_ROLES_GATING"
      Then the message "You don't have permission to view roles" should be displayed
      And the roles table should not be displayed

    Scenario: A member with only roles:read can view roles but not change them
      Given the user is a member of "E2E_ROLES_GATING" with only the "roles:read" permission
      When the user opens the "Roles" section of the Settings page of "E2E_ROLES_GATING"
      Then the roles table should list the roles "Admin", "Editor" and "Viewer"
      And the "New role" button should not be visible
      And the "Editor" row should not offer "Edit role" or "Delete role"

    Scenario: A member with roles:write can create, edit and delete roles
      Given the user is a member of "E2E_ROLES_GATING" with the "roles:read" and "roles:write" permissions
      When the user opens the "Roles" section of the Settings page of "E2E_ROLES_GATING"
      Then the "New role" button should be visible
      And the "Editor" row should offer "Edit role" and "Delete role"
      And the built-in "Admin" row should offer "Edit role" but not "Delete role"
