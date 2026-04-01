@admin @users
Feature: User management
  Admins should be able to view user accounts, create new users, update
  profile details and roles, reset passwords, and delete removable users.
  Sensitive actions should clearly communicate their impact before they are
  confirmed.

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

    Scenario: Opening the create-user dialog
      When the user clicks the "New User" button
      Then the "Create User" dialog should open
      And the dialog should explain that a secure temporary password will be generated automatically
      And the dialog should contain "Username", "Full Name", and "Role" fields

    Scenario: Submitting an empty form shows validation
      When the user clicks the "New User" button
      And the user clicks "Create user"
      Then a validation message should indicate that the full name is required

    Scenario: Username is required when the full name is present
      When the user clicks the "New User" button
      And the user fills the full name with "Jane Doe"
      And the user clicks "Create user"
      Then a validation message should indicate that the username is required
      And the dialog should remain open

    Scenario: Role picker lists the available roles
      When the user clicks the "New User" button
      And the user opens the role picker
      Then the role picker should list "ADMIN"
      And the role picker should list "SUPER_ADMIN"
      And the role picker should list "USER"

    Scenario: Creating a user without selecting a role defaults to USER and requires a password change on first login
      When the user clicks the "New User" button
      And the user fills the username with "BDD_USER_DEFAULT_ROLE"
      And the user fills the full name with "BDD User Default Role"
      And the user clicks "Create user"
      Then the "User created" dialog should appear
      And the dialog should show a one-time temporary password
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

    Scenario: Creating a user with an explicitly selected role
      When the user clicks the "New User" button
      And the user fills the username with "BDD_ADMIN_USER"
      And the user fills the full name with "BDD Admin User"
      And the user selects the role "ADMIN"
      And the user clicks "Create user"
      Then the "User created" dialog should appear
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
      And the role field should be pre-filled with that user's current role

    Scenario: Saving updated full name and role
      When the user clicks the edit action for "EDITABLE_USER"
      And the user changes the full name to "Edited User Name"
      And the user changes the role to "ADMIN"
      And the user clicks "Save changes"
      Then the dialog should close
      And the user "EDITABLE_USER" should appear in the users table
      And the user row should show the full name "Edited User Name"
      And the user row should show the role "ADMIN"

    Scenario: Cancelling the edit dialog discards changes
      When the user clicks the edit action for "EDITABLE_USER"
      And the user changes the full name to "Unsaved Name"
      And the user closes the dialog
      Then the dialog should close
      And the user row for "EDITABLE_USER" should not show the full name "Unsaved Name"

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