@validation
Feature: Login form validation
  The login form should provide immediate feedback for missing or invalid input.

  Background:
    Given the browser starts with a clean unauthenticated session
    And the user is on the login page

  Scenario: Username is required after leaving the field empty
    When the user focuses the username field
    And the user moves focus to the password field
    Then the validation message "Username is required" should be visible

  Scenario: Username is required after clearing a previously entered value
    When the user fills the username field with "someuser"
    And the user clears the username field
    And the user moves focus to the password field
    Then the validation message "Username is required" should be visible

  Scenario: Username must be at least three characters
    When the user fills the username field with "ab"
    And the user fills the password field with "validpass1"
    Then the validation message "Username must be at least 3 characters" should be visible
    And the sign-in button should be enabled

  Scenario: Sign-in is enabled only when both fields contain values
    When the user fills the username field with the configured valid username
    Then the sign-in button should be disabled
    When the user fills the password field with the configured valid password
    Then the sign-in button should be enabled
    When the user clears the password field
    Then the sign-in button should be disabled