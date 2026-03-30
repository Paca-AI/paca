@session
Feature: Session management
  Authenticated sessions should persist only where expected and should be invalidated cleanly on logout.

  @authenticated
  Rule: Pre-authenticated session behavior

    Background:
      Given the user already has a stored authenticated session

    Scenario: Logout redirects back to the login page
      When the user opens the home page
      And the user clicks the logout button
      Then the username field should be visible
      And the password field should be visible
      And the sign-in button should be visible

    Scenario: Browser back navigation after logout does not restore the home page
      When the user opens the home page
      And the user clicks the logout button
      And the user navigates back in the browser
      Then the username field should be visible
      And the sign-in button should be visible

    Scenario: Session persists across a page reload
      When the user opens the home page
      And the user reloads the page
      Then the home heading should be visible

    Scenario: Session is shared across tabs in the same browser context
      When the user opens the home page
      And the user opens a second tab in the same browser context
      And the user navigates the second tab to the application root
      Then the home heading should be visible in the second tab

  @fresh-context
  Rule: Fresh browser context behavior

    Scenario: Closing the browser context clears the session
      Given the user signs in successfully in a fresh browser context
      When the user closes that browser context
      And the user opens a new browser context
      And the user navigates to the application root
      Then the username field should be visible
      And the sign-in button should be visible

    Scenario: Independent browser contexts do not share authentication state
      When the user opens a brand-new browser context
      And the user navigates to the application root
      Then the username field should be visible
      And the sign-in button should be visible