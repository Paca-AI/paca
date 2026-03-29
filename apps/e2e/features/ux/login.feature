@ux
Feature: Login page user experience
  The login page should remain usable across common interactions, themes, and mobile layouts.

  Background:
    Given the browser starts with a clean unauthenticated session
    And the user is on the login page

  Scenario: An error is cleared by a successful retry
    When the user signs in with username "invaliduser" and password "invalidpass"
    Then the login error message should be visible
    When the user signs in with the configured valid username and password
    Then the user should be redirected to the home page

  Scenario: Password visibility can be toggled with the show and hide controls
    When the user fills the password field with "e2e-admin-password"
    Then the show-password control should be visible
    When the user clicks the show-password control
    Then the hide-password control should be visible
    When the user clicks the hide-password control
    Then the show-password control should be visible

  Scenario: Remember me can be enabled before a successful login
    When the user enables remember me
    Then the remember-me control should be checked
    When the user signs in with the configured valid username and password
    Then the user should be redirected to the home page

  Scenario: Theme mode cycles from auto to light to dark without breaking the form
    When the user changes the theme mode from auto to light
    Then the light theme toggle should be visible
    When the user changes the theme mode from light to dark
    Then the dark theme toggle should be visible
    And the login form should remain visible and usable

  Scenario: The login form fits within an iPhone 8 viewport
    When the user resizes the viewport to 375 by 667
    And the user reloads the login page
    Then the "Welcome back" heading should be visible
    And the login form should remain visible and usable
    And the page should not have horizontal scrolling

  Scenario: Touch interactions can complete a successful login on mobile viewport
    When the user resizes the viewport to 375 by 667
    And the user reloads the login page
    And the user signs in on mobile with the configured valid username and password
    Then the home heading should be visible