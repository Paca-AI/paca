@auth
Feature: Login authentication
  The login form should allow valid users to sign in and block invalid or incomplete submissions.

  Background:
    Given the browser starts with a clean unauthenticated session
    And the user is on the login page

  Scenario: Valid credentials redirect to the home page
    When the user signs in with the configured valid username and password
    Then the user should be redirected to the home page

  Scenario: Invalid username shows an error and preserves entered values
    When the user signs in with username "nonexistentuser" and password "wrongpassword"
    Then the login error message should be visible
    And the user should remain on the login page
    And the username field should contain "nonexistentuser"
    And the password field should contain "wrongpassword"

  Scenario: Invalid password shows an error and preserves entered values
    When the user signs in with the configured valid username and password "wrongpassword123"
    Then the login error message should be visible
    And the user should remain on the login page
    And the username field should contain the configured valid username
    And the password field should contain "wrongpassword123"

  Scenario: Sign-in is disabled when both fields are empty
    Then the sign-in button should be disabled

  Scenario: Sign-in is disabled when the password is missing
    When the user fills the username field with the configured valid username
    Then the sign-in button should be disabled