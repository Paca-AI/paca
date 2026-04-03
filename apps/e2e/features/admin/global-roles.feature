@admin @global-roles
Feature: Global roles management
  Admins should be able to list, create, edit, and delete global roles
  with granular permission assignments.  Users without the required
  permission should be blocked at the route level.

  @authenticated
  Rule: Viewing the roles list

    Background:
      Given the user already has a stored authenticated admin session
      And the user navigates to the global roles page

    Scenario: Page header and statistics are visible
      Then the "Global Roles" page heading should be visible
      And the statistics bar should show the total number of roles
      And the statistics bar should show the total permission grants across all roles

    Scenario: Roles table displays expected columns and rows
      Then the roles table should have columns "Name", "Permissions", and "Created"
      And each default role should appear as a row in the table

    Scenario: "New Role" button is displayed for users with write permission
      Then the "New Role" button should be visible

    Scenario: Loading skeleton is replaced by the table once data arrives
      When the page first loads
      Then the loading skeleton should be visible briefly
      And the roles table should appear once loading completes

  @authenticated
  Rule: Creating a global role

    Background:
      Given the user already has a stored authenticated admin session
      And the user is on the global roles page

    Scenario: Opening the create-role dialog
      When the user clicks the "New Role" button
      Then the role form dialog should open
      And the dialog title should be "Create Role"
      And the name field should be empty
      And all permission switches should be in their default state

    Scenario: Creating a role with a name and selected permissions
      When the user clicks the "New Role" button
      And the user fills the role name with "SECURITY_MANAGER"
      And the user enables the "Read Global Roles" permission
      And the user enables the "Read Users" permission
      And the user clicks "Create role"
      Then the dialog should close
      And the role "SECURITY_MANAGER" should appear in the roles table
      And the statistics bar should reflect the updated role count

    Scenario: Submitting without a name is blocked
      When the user clicks the "New Role" button
      And the user leaves the role name empty
      Then the "Create role" submit button should be disabled

    Scenario: Cancelling the dialog discards changes
      When the user clicks the "New Role" button
      And the user fills the role name with "SHOULD_NOT_EXIST"
      And the user closes the dialog
      Then the role "SHOULD_NOT_EXIST" should not appear in the roles table

    Scenario: Creating a role without any permissions is allowed
      When the user clicks the "New Role" button
      And the user fills the role name with "EMPTY_PERMISSIONS_ROLE"
      And the user clicks "Create role"
      Then the role "EMPTY_PERMISSIONS_ROLE" should appear in the roles table
      And the role should show zero active permissions

    Scenario: Enabling all permissions in a domain group collapses to a wildcard
      When the user clicks the "New Role" button
      And the user fills the role name with "GLOBAL_ROLES_WILDCARD_ROLE"
      And the user enables the "Read Global Roles" permission
      And the user enables the "Write Global Roles" permission
      And the user enables the "Assign Global Roles" permission
      And the user clicks "Create role"
      Then the dialog should close
      And the role "GLOBAL_ROLES_WILDCARD_ROLE" should appear in the roles table
      And the role should show 1 active permission representing "global_roles.*"

    Scenario: Enabling all users permissions collapses to users wildcard
      When the user clicks the "New Role" button
      And the user fills the role name with "USERS_WILDCARD_ROLE"
      And the user enables the "Read Users" permission
      And the user enables the "Write Users" permission
      And the user enables the "Delete Users" permission
      And the user clicks "Create role"
      Then the dialog should close
      And the role "USERS_WILDCARD_ROLE" should appear in the roles table
      And the role should show 1 active permission representing "users.*"

    Scenario: Enabling all projects permissions collapses to projects wildcard
      When the user clicks the "New Role" button
      And the user fills the role name with "PROJECTS_WILDCARD_ROLE"
      And the user enables the "Read All Projects" permission
      And the user enables the "Create Projects" permission
      And the user enables the "Write Projects" permission
      And the user enables the "Delete Projects" permission
      And the user enables the "Read Project Members" permission
      And the user enables the "Write Project Members" permission
      And the user enables the "Read Project Roles" permission
      And the user enables the "Write Project Roles" permission
      And the user clicks "Create role"
      Then the dialog should close
      And the role "PROJECTS_WILDCARD_ROLE" should appear in the roles table
      And the role should show 1 active permission representing "projects.*"

    Scenario: Enabling all permissions across all groups collapses each domain independently
      When the user clicks the "New Role" button
      And the user fills the role name with "SUPER_ROLE"
      And the user enables the "Read Global Roles" permission
      And the user enables the "Write Global Roles" permission
      And the user enables the "Assign Global Roles" permission
      And the user enables the "Read Users" permission
      And the user enables the "Write Users" permission
      And the user enables the "Delete Users" permission
      And the user enables the "Read All Projects" permission
      And the user enables the "Create Projects" permission
      And the user enables the "Write Projects" permission
      And the user enables the "Delete Projects" permission
      And the user enables the "Read Project Members" permission
      And the user enables the "Write Project Members" permission
      And the user enables the "Read Project Roles" permission
      And the user enables the "Write Project Roles" permission
      And the user clicks "Create role"
      Then the dialog should close
      And the role "SUPER_ROLE" should appear in the roles table
      And the role should show 3 active permissions representing "global_roles.*", "users.*", and "projects.*"

    Scenario: Creating a role with permissions from multiple groups
      When the user clicks the "New Role" button
      And the user fills the role name with "MIXED_ROLE"
      And the user enables the "Write Global Roles" permission
      And the user enables the "Read Users" permission
      And the user clicks "Create role"
      Then the dialog should close
      And the role "MIXED_ROLE" should appear in the roles table
      And the role should show 2 active permissions

    Scenario: Toggling a permission on then off leaves it disabled
      When the user clicks the "New Role" button
      And the user enables the "Read Users" permission
      And the user disables the "Read Users" permission
      Then the "Read Users" permission switch should be off

  @authenticated
  Rule: Permission management in the role form dialog

    Background:
      Given the user already has a stored authenticated admin session
      And the user is on the global roles page

    Scenario: Permission switches are organised into domain groups
      When the user clicks the "New Role" button
      Then the role form dialog should open
      And the permission section should display a "Global Roles" group
      And the permission section should display a "Users" group
      And the permission section should display a "Projects" group

    Scenario: Each group in the Projects domain is labelled correctly
      When the user clicks the "New Role" button
      Then the "Projects" group should contain "Read All Projects", "Create Projects", "Write Projects", "Delete Projects", "Read Project Members", "Write Project Members", "Read Project Roles", and "Write Project Roles" permissions

    Scenario: Each permission switch shows a label and description
      When the user clicks the "New Role" button
      Then the "Read Global Roles" permission should show description "View global role definitions"
      And the "Write Global Roles" permission should show description "Create and update global role definitions"
      And the "Assign Global Roles" permission should show description "Assign global roles to users"
      And the "Read Users" permission should show description "View user profiles and list"
      And the "Write Users" permission should show description "Create and update user accounts"
      And the "Delete Users" permission should show description "Remove user accounts"
      And the "Read All Projects" permission should show description "View all projects in the workspace"
      And the "Create Projects" permission should show description "Create new projects"
      And the "Write Projects" permission should show description "Update project details"
      And the "Delete Projects" permission should show description "Permanently delete projects"
      And the "Read Project Members" permission should show description "View members of any project"
      And the "Write Project Members" permission should show description "Add, remove, and update members in any project"
      And the "Read Project Roles" permission should show description "View roles defined in any project"
      And the "Write Project Roles" permission should show description "Create and update roles in any project"

    Scenario: All permission switches are off by default in the create dialog
      When the user clicks the "New Role" button
      Then the "Read Global Roles" permission switch should be off
      And the "Write Global Roles" permission switch should be off
      And the "Assign Global Roles" permission switch should be off
      And the "Read Users" permission switch should be off
      And the "Write Users" permission switch should be off
      And the "Delete Users" permission switch should be off
      And the "Read All Projects" permission switch should be off
      And the "Create Projects" permission switch should be off
      And the "Write Projects" permission switch should be off
      And the "Delete Projects" permission switch should be off
      And the "Read Project Members" permission switch should be off
      And the "Write Project Members" permission switch should be off
      And the "Read Project Roles" permission switch should be off
      And the "Write Project Roles" permission switch should be off

    Scenario: Enabling a permission updates the switch to on
      When the user clicks the "New Role" button
      And the user enables the "Assign Global Roles" permission
      Then the "Assign Global Roles" permission switch should be on
      And all other permission switches should remain off

    Scenario: Permissions count in the table matches granted permissions
      When the user clicks the "New Role" button
      And the user fills the role name with "COUNT_CHECK_ROLE"
      And the user enables the "Read Global Roles" permission
      And the user enables the "Delete Users" permission
      And the user clicks "Create role"
      Then the role "COUNT_CHECK_ROLE" should show 2 active permissions
      And the statistics bar should reflect the added permission grants

    Scenario: Closing and reopening the dialog resets permission state
      When the user clicks the "New Role" button
      And the user enables the "Read Users" permission
      And the user closes the dialog
      And the user clicks the "New Role" button again
      Then all permission switches should be in their default state

  @authenticated
  Rule: Editing a global role

    Background:
      Given the user already has a stored authenticated admin session
      And the user is on the global roles page
      And a custom role named "EDITABLE_ROLE" exists

    Scenario: Opening the edit dialog pre-populates current data
      When the user hovers over the "EDITABLE_ROLE" row
      And the user clicks the edit button for that role
      Then the role form dialog should open
      And the dialog title should be "Edit role"
      And the name field should be pre-filled with "EDITABLE_ROLE"
      And the permission switches should reflect the role's current permissions

    Scenario: Saving updated role name and permissions
      When the user hovers over the "EDITABLE_ROLE" row
      And the user clicks the edit button for that role
      And the user clears the role name and types "RENAMED_ROLE"
      And the user toggles the "Delete Users" permission
      And the user clicks "Save changes"
      Then the dialog should close
      And the role "RENAMED_ROLE" should appear in the roles table

    Scenario: Cancelling the edit dialog discards all changes
      When the user hovers over the "EDITABLE_ROLE" row
      And the user clicks the edit button for that role
      And the user clears the role name and types "UNSAVED_CHANGE"
      And the user closes the dialog
      Then the role "EDITABLE_ROLE" should still appear in the roles table
      And the role "UNSAVED_CHANGE" should not appear in the roles table

    Scenario: Enabling additional permissions on an existing role without filling the domain
      Given a custom role named "EDITABLE_ROLE" exists with only "Read Global Roles" permission
      When the user hovers over the "EDITABLE_ROLE" row
      And the user clicks the edit button for that role
      And the user enables the "Write Global Roles" permission
      And the user enables the "Read Users" permission
      And the user clicks "Save changes"
      Then the dialog should close
      And the role "EDITABLE_ROLE" should show 3 active permissions

    Scenario: Completing a domain group during edit collapses it to a wildcard
      Given a custom role named "EDITABLE_ROLE" exists with "Read Global Roles" and "Write Global Roles" permissions
      When the user hovers over the "EDITABLE_ROLE" row
      And the user clicks the edit button for that role
      And the user enables the "Assign Global Roles" permission
      And the user clicks "Save changes"
      Then the dialog should close
      And the role "EDITABLE_ROLE" should show 1 active permission representing "global_roles.*"

    Scenario: Removing all permissions from an existing role
      Given a custom role named "EDITABLE_ROLE" exists with "Read Global Roles" and "Read Users" permissions
      When the user hovers over the "EDITABLE_ROLE" row
      And the user clicks the edit button for that role
      And the user disables the "Read Global Roles" permission
      And the user disables the "Read Users" permission
      And the user clicks "Save changes"
      Then the dialog should close
      And the role "EDITABLE_ROLE" should show zero active permissions

    Scenario: Edit dialog pre-populates the correct permission switches
      Given a custom role named "EDITABLE_ROLE" exists with "Assign Global Roles" and "Delete Users" permissions
      When the user hovers over the "EDITABLE_ROLE" row
      And the user clicks the edit button for that role
      Then the "Assign Global Roles" permission switch should be on
      And the "Delete Users" permission switch should be on
      And the "Read Global Roles" permission switch should be off
      And the "Write Global Roles" permission switch should be off
      And the "Read Users" permission switch should be off

    Scenario: Toggling a permission off during edit persists after save
      Given a custom role named "EDITABLE_ROLE" exists with "Read Users" permission
      When the user hovers over the "EDITABLE_ROLE" row
      And the user clicks the edit button for that role
      And the user disables the "Read Users" permission
      And the user clicks "Save changes"
      Then the dialog should close
      When the user hovers over the "EDITABLE_ROLE" row
      And the user clicks the edit button for that role
      Then the "Read Users" permission switch should be off

  @authenticated
  Rule: Deleting a global role

    Background:
      Given the user already has a stored authenticated admin session
      And the user is on the global roles page
      And a custom role named "DELETABLE_ROLE" exists

    Scenario: Confirming deletion removes the role
      When the user hovers over the "DELETABLE_ROLE" row
      And the user clicks the delete button for that role
      Then the delete confirmation dialog should open
      And the dialog should display the name "DELETABLE_ROLE"
      When the user confirms deletion
      Then the role "DELETABLE_ROLE" should no longer appear in the roles table
      And the statistics bar should reflect the updated role count

    Scenario: Cancelling the delete dialog preserves the role
      When the user hovers over the "DELETABLE_ROLE" row
      And the user clicks the delete button for that role
      And the user cancels the deletion
      Then the role "DELETABLE_ROLE" should still appear in the roles table

  @authenticated
  Rule: Permission-based access control

    Scenario: Read-only admin sees the table but no modification controls
      Given the user already has a stored session with only "global_roles.read" permission
      And the user navigates to the global roles page
      Then the roles table should be visible
      And the "New Role" button should not be visible
      And no edit or delete buttons should be visible on any role row

    Scenario: User without global_roles.read is redirected
      Given the user already has a stored session without any global roles permission
      When the user navigates to the global roles page
      Then the user should be redirected to the home page

  @authenticated
  Rule: Empty and error states

    Scenario: Empty state prompts role creation
      Given the user already has a stored authenticated admin session
      And there are no roles configured in the system
      When the user navigates to the global roles page
      Then the empty state should be visible
      And the empty state message should read "No roles defined yet"
      And the empty state should contain a "Create role" button

    Scenario: Create role from empty state opens the dialog
      Given the user already has a stored authenticated admin session
      And there are no roles configured in the system
      And the user is on the global roles page
      When the user clicks "Create role" in the empty state
      Then the role form dialog should open

    Scenario: Error state is shown when the API fails
      Given the user already has a stored authenticated admin session
      And the global roles API is unavailable
      When the user navigates to the global roles page
      Then the error state should be visible
