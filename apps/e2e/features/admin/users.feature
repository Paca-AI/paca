@admin @users
Feature: User management
  Admins should be able to view user accounts, create new users, update
  profile details, change roles, reset passwords, and delete removable users.
  Sensitive actions should clearly communicate their impact before they are
  confirmed.

  A role is its own permission (global_roles.assign), separate from managing
  users (users.write): creating or editing a user never carries one. Creating a
  user is a short wizard - 1 Details, 2 Role, 3 Password. The first step's button
  creates the account, which starts with the default global role; the role step
  is a separate request that changes it, and the one-time password comes last.
  Someone who may not assign roles gets 1 Details, 2 Password. A role is changed
  later from the role button in the users table.

  @authenticated
  Rule: Viewing the users list

    Background:
      Given the user already has a stored authenticated admin session
      And the user navigates to the users page

    Scenario: Page header and summary are visible
      Then the "User Management" page heading should be visible
      And the page description should mention managing user accounts and assigned roles
      And the "New User" button should be visible
      And the users summary should show the total number of users in the system

    Scenario: Users table displays expected columns and current admin
      Then the users table should have columns "Username", "Full Name", "Role", and "Created"
      And the current signed-in administrator should appear in the users table
      And the administrator row should show the role "SUPER_ADMIN"

    Scenario: Protected accounts do not expose a delete action
      Then the current signed-in administrator row should show the "Edit user" action
      And the current signed-in administrator row should show the "Reset password" action
      And the current signed-in administrator row should not show the "Delete user" action

  @authenticated
  Rule: Creating a user

    Background:
      Given the user already has a stored authenticated admin session
      And the user is on the users page

    Scenario: Opening the create-user dialog on its details step
      When the user clicks the "New User" button
      Then the "Create User" dialog should open on step 1 of 3
      And the dialog should explain that a secure temporary password will be generated automatically
      And the dialog should contain "Username" and "Full Name" fields
      And the dialog should not contain a role field
      And the dialog should offer "Create user" rather than "Continue"

    Scenario: Creating with an empty form shows validation
      When the user clicks the "New User" button
      And the user clicks "Create user"
      Then a validation message should indicate that the full name is required

    Scenario: Username is required when the full name is present
      When the user clicks the "New User" button
      And the user fills the full name with "Jane Doe"
      And the user clicks "Create user"
      Then a validation message should indicate that the username is required
      And the dialog should remain on step 1 of 3

    Scenario: The account is created first, then the role step lists the available roles with the default one held
      When the user clicks the "New User" button
      And the user fills the username with "BDD_ROLE_STEP"
      And the user fills the full name with "BDD Role Step"
      And the user clicks "Create user"
      Then the dialog should show step 2 of 3
      And the dialog should say that "BDD_ROLE_STEP" was created with the "USER" role
      And the role step should list "ADMIN"
      And the role step should list "SUPER_ADMIN"
      And the role step should list "USER"
      And "USER" should be selected and marked as both the current role and the default
      And the user "BDD_ROLE_STEP" should exist with the role "USER"
      And the dialog should not offer "Back" or "Cancel"
      And the dialog should not show the temporary password yet

    Scenario: Closing the dialog on the role step carries on to the password instead of losing it
      When the user clicks the "New User" button
      And the user fills the username with "BDD_CLOSE_ROLE_STEP"
      And the user fills the full name with "BDD Close Role Step"
      And the user clicks "Create user"
      And the user closes the dialog
      Then the "User created" dialog should appear on step 3 of 3
      And the dialog should show a one-time temporary password
      When the user closes the success dialog
      Then the user row should show the role "USER"

    Scenario: Creating a user with the default USER role and requiring a password change on first login
      When the user clicks the "New User" button
      And the user fills the username with "BDD_USER_DEFAULT_ROLE"
      And the user fills the full name with "BDD User Default Role"
      And the user clicks "Create user"
      And the user clicks "Continue" on the role step
      Then the "User created" dialog should appear on step 3 of 3
      And the dialog should show a one-time temporary password
      And the dialog should show the role "USER"
      And the user stores the generated temporary password for "BDD_USER_DEFAULT_ROLE"
      And the dialog should warn that the password will not be shown again
      When the user closes the success dialog
      Then the user "BDD_USER_DEFAULT_ROLE" should appear in the users table
      And the user row should show the role "USER"
      And the users summary should reflect the added user
      And the user row should indicate that a password reset is required
      When the user signs out
      And the user signs in as "BDD_USER_DEFAULT_ROLE" with the stored temporary password
      Then the forced password change form should be visible
      And the page should explain that the password must be changed before continuing
      When the user changes the password for "BDD_USER_DEFAULT_ROLE" to "BDDUserDefaultRole123!"
      Then the user should be redirected to the home page
      When the user signs out
      And the user signs in as "BDD_USER_DEFAULT_ROLE" with password "BDDUserDefaultRole123!"
      Then the user should be redirected to the home page

    Scenario: Assigning a selected role through its own step shows the password last
      When the user clicks the "New User" button
      And the user fills the username with "BDD_ADMIN_USER"
      And the user fills the full name with "BDD Admin User"
      And the user clicks "Create user"
      And the user selects the role "ADMIN" on the role step
      And the user clicks "Assign role"
      Then the "User created" dialog should appear on step 3 of 3
      And the dialog should show the role "ADMIN"
      And the dialog should show a one-time temporary password
      When the user closes the success dialog
      Then the user "BDD_ADMIN_USER" should appear in the users table
      And the user row should show the role "ADMIN"

    Scenario: Cancelling the create-user dialog discards changes
      When the user clicks the "New User" button
      And the user fills the username with "SHOULD_NOT_EXIST"
      And the user fills the full name with "Should Not Exist"
      And the user closes the dialog
      Then the user "SHOULD_NOT_EXIST" should not appear in the users table

  @authenticated
  Rule: Editing a user

    Background:
      Given the user already has a stored authenticated admin session
      And the user is on the users page
      And a user named "EDITABLE_USER" exists

    Scenario: Opening the edit dialog pre-populates current values
      When the user clicks the edit action for "EDITABLE_USER"
      Then the "Edit User" dialog should open
      And the full name field should be pre-filled with that user's current name
      And the dialog should not contain a role field
      And the dialog should be a single step rather than a wizard

    Scenario: Saving the updated full name
      When the user clicks the edit action for "EDITABLE_USER"
      And the user changes the full name to "Edited User Name"
      And the user clicks "Save changes"
      Then the dialog should close
      And the user "EDITABLE_USER" should appear in the users table
      And the user row should show the full name "Edited User Name"
      And the user row should keep the role "USER"

    Scenario: Cancelling the edit dialog discards changes
      When the user clicks the edit action for "EDITABLE_USER"
      And the user changes the full name to "Unsaved Name"
      And the user closes the dialog
      Then the dialog should close
      And the user row for "EDITABLE_USER" should not show the full name "Unsaved Name"

  @authenticated
  Rule: Changing a user's role

    Background:
      Given the user already has a stored authenticated admin session
      And the user is on the users page
      And a user named "ROLE_USER" exists

    Scenario: The role in the users table opens a dialog of its own
      When the user clicks the role of "ROLE_USER"
      Then the "Change role" dialog should open
      And the dialog should say to choose a role for "ROLE_USER"
      And the dialog should list the available roles with the current role "USER" selected and marked as current
      And "Assign role" should be disabled until a different role is chosen

    Scenario: Assigning another role updates the user
      When the user clicks the role of "ROLE_USER"
      And the user chooses the role "ADMIN"
      And the user clicks "Assign role"
      Then the dialog should close
      And the user row should show the role "ADMIN"

    Scenario: A full-access role is flagged before it is assigned
      When the user clicks the role of "ROLE_USER"
      And the user chooses the role "SUPER_ADMIN"
      Then the dialog should warn that the role has full access
      When the user clicks "Cancel"
      Then the user row should show the role "USER"

    Scenario: Changing your own role warns that access can be lost
      When the user clicks the role of the signed-in administrator
      And the user chooses the role "USER"
      Then the dialog should warn that this is their own account and access to the page can be lost
      When the user clicks "Cancel"
      Then the administrator row should show the role "SUPER_ADMIN"

  Rule: Roles are a separate permission from managing users

    Scenario: Writing users without assigning roles gives a two-step wizard and a plain role label
      Given the user has the "users.read" and "users.write" global permissions
      And the user does not have the "global_roles.assign" global permission
      When the user navigates to the users page
      Then the roles in the users table should be plain text rather than buttons
      When the user clicks the "New User" button
      Then the "Create User" dialog should open on step 1 of 2
      And the dialog should offer "Create user" rather than "Continue"
      When the user creates the user "BDD_NO_ROLE_STEP"
      Then the "User created" dialog should appear on step 2 of 2
      And the dialog should show a one-time temporary password
      And the dialog should show the role "USER"

    Scenario: Assigning roles without writing users offers the role button but no user editing
      Given the user has the "users.read", "global_roles.read" and "global_roles.assign" global permissions
      And the user does not have the "users.write" global permission
      And a user named "TARGET_USER" exists
      When the user navigates to the users page
      Then the "New User" button, the edit actions and the reset password actions should not be visible
      When the user changes the role of "TARGET_USER" to "ADMIN"
      Then the user row should show the role "ADMIN"

  @authenticated
  Rule: Resetting a password

    Background:
      Given the user already has a stored authenticated admin session
      And the user is on the users page
      And a user named "RESETTABLE_USER" exists

    Scenario: Opening the reset-password confirmation dialog
      When the user clicks the reset password action for "RESETTABLE_USER"
      Then the "Reset password" dialog should open
      And the dialog should explain that a strong temporary password will be generated
      And the dialog should explain that the user must change it on next login

    Scenario: Cancelling password reset leaves the user unchanged
      When the user clicks the reset password action for "RESETTABLE_USER"
      And the user clicks "Cancel"
      Then the dialog should close
      And the user "RESETTABLE_USER" should remain in the users table

    Scenario: Confirming password reset shows a new temporary password and forces a password change on next login
      When the user clicks the reset password action for "RESETTABLE_USER"
      And the user clicks "Reset password"
      Then a password reset success dialog should appear
      And the dialog should show a one-time temporary password
      And the user stores the generated temporary password for "RESETTABLE_USER"
      And the dialog should allow the password to be copied
      When the user closes the success dialog
      Then the user "RESETTABLE_USER" should remain in the users table
      And the user row should indicate that a password reset is required
      When the user signs out
      And the user signs in as "RESETTABLE_USER" with the stored temporary password
      Then the forced password change form should be visible
      And the page should explain that the password must be changed before continuing
      When the user changes the password for "RESETTABLE_USER" to "ResettableUser123!"
      Then the user should be redirected to the home page
      When the user signs out
      And the user signs in as "RESETTABLE_USER" with password "ResettableUser123!"
      Then the user should be redirected to the home page

  @authenticated
  Rule: Deleting a user

    Background:
      Given the user already has a stored authenticated admin session
      And the user is on the users page
      And a user named "DELETABLE_USER" exists

    Scenario: Opening the delete confirmation dialog
      When the user clicks the delete action for "DELETABLE_USER"
      Then the "Delete user" dialog should open
      And the dialog should warn that the account will be permanently removed
      And the dialog should warn that the action cannot be undone

    Scenario: Cancelling deletion keeps the user
      When the user clicks the delete action for "DELETABLE_USER"
      And the user clicks "Cancel"
      Then the dialog should close
      And the user "DELETABLE_USER" should remain in the users table

    Scenario: Confirming deletion removes the user
      When the user clicks the delete action for "DELETABLE_USER"
      And the user clicks "Delete user"
      Then the dialog should close
      And the user "DELETABLE_USER" should no longer appear in the users table
      And the users summary should reflect the removed user