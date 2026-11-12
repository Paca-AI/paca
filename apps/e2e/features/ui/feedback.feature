@ui @feedback
Feature: Feedback and issue reporting
  Users can open a GitHub issue straight from the app, either to send general
  feedback or to report an error they just hit.

  @authenticated
  Rule: Sending feedback from the user menu

    Background:
      Given the user already has a stored authenticated session
      And the user is on the home page

    Scenario: The user menu opens the GitHub issue chooser
      When the user opens the profile menu in the sidebar
      And the user clicks "Send feedback"
      Then a new tab should open on the repository's "issues/new/choose" page
      And the app should stay open in the original tab

  @authenticated
  Rule: Reporting an error with its full details

    Scenario: An error screen offers a pre-filled bug report
      Given a page's data request fails with an error
      When the route error screen is shown
      Then the screen should offer a "Report this issue" button
      When the user clicks "Report this issue"
      Then a new tab should open on the "bug_report.yml" issue form
      And the title should start with "[Bug] "
      And the "logs" field should contain the error message and the failed request
      And the "area" field should be "Frontend"

    Scenario: A permission error does not offer the report button
      Given a page's data request is rejected with a 403
      When the route error screen is shown
      Then no "Report this issue" button should be shown
