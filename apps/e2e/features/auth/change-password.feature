@auth @password-change
Feature: Forced password change
  Users who sign in with a temporary password should be blocked from the workspace until they set a new personal password.

  Rule: A temporary password routes the user to the password change screen

    Scenario: First sign-in after account creation requires a password change
      Given an administrator has created a new user account and stored the generated temporary password
      And the browser starts with a clean unauthenticated session
      When the user signs in with that temporary password
      Then the user should be redirected to the change-password page
      And the page should show the "Secure your account" guidance
      And the page should explain that the user must set a new personal password before accessing the workspace
      And the page should show the step "Enter the temporary password you received"
      And the page should show the step "Choose a strong new password"
      And the page should show the step "Sign in again with your new password"
      And the workspace home page should not be visible

    Scenario: First sign-in after a password reset requires a password change
      Given an administrator has reset the password for an existing user and stored the generated temporary password
      And the browser starts with a clean unauthenticated session
      When the user signs in with that temporary password
      Then the user should be redirected to the change-password page
      And the "Set new password" form should be visible
      And the workspace home page should not be visible

  Rule: The password change form prevents incomplete or inconsistent submissions

    Background:
      Given the user is on the change-password page after signing in with a temporary password

    Scenario: The form shows the required fields and password visibility controls
      Then the "Current password" field should be visible
      And the "New password" field should be visible
      And the "Confirm new password" field should be visible
      And the "Show current password" control should be visible
      And the "Show new password" control should be visible
      And the "Show confirm password" control should be visible
      And the "Change password" button should be disabled

    Scenario: The form remains blocked until all password fields are complete and matching
      When the user fills only the current password
      Then the "Change password" button should be disabled
      When the user fills a new password that is different from the confirmation
      Then the "Change password" button should be disabled
      When the user updates the confirmation to match the new password
      Then the "Change password" button should be enabled

    Scenario: Signing out does not bypass the password change requirement
      When the user clicks "Sign out"
      Then the user should be redirected to the login page
      When the user signs in again with the same temporary password
      Then the user should be redirected to the change-password page

  Rule: Completing the password change unlocks the account

    Background:
      Given the user is on the change-password page after signing in with a temporary password

    Scenario: A successful password change returns the user to sign-in
      When the user changes the password from the temporary password to a valid personal password
      Then the user should be redirected to the login page
      And the temporary password should no longer grant access

    Scenario: The new password grants access to the workspace
      When the user changes the password from the temporary password to a valid personal password
      And the user signs in with the new password
      Then the user should be redirected to the home page