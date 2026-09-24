@projects @jev
Feature: Jev AI project settings
  Jev is an optional, per-project AI decision engine that powers task
  auto-fill, task auto-assign, and "Jev Condition" nodes in automations.
  Each project brings its own Jev-compatible provider credentials (API key,
  optional host and model), managed from Settings > Jev AI. The API key is
  write-only: it is never shown again after saving. Every Jev-dependent
  affordance stays hidden until the project has a key configured.

  The E2E stack has no real Jev provider, so the connection test's result
  is stubbed at the network layer; saving credentials and settings is real.

  @authenticated
  Rule: Unconfigured project

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_JEV_..." exists without Jev credentials

    Scenario: Jev AI section shows the not-configured state
      When the user opens Settings and clicks "Jev AI" in the settings sidebar
      Then the "Jev AI" section heading should be visible
      And the credentials badge should read "Not configured"
      And the "Jev isn't configured yet" empty state should be visible
      And the auto-fill and auto-assign controls should not be visible
      And the "Test connection" button should not be visible

    Scenario: The Jev Condition node is hidden from the automation palette
      Given an automation exists in the project
      When the user opens the automation builder and clicks "Add Condition"
      Then the "Jev Condition" option should not be offered

  @authenticated
  Rule: Configuring credentials

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_JEV_..." exists without Jev credentials

    Scenario: Saving credentials enables Jev and never reveals the key
      When the user enters an API key, a host and a model
      And clicks "Save credentials"
      Then "Saved ✓" should be shown
      And the credentials badge should read "Configured"
      And the API key field should be empty with a "keep the current key" placeholder
      And the auto-fill and auto-assign controls should be visible
      And after reloading, the host and model should still be filled in
      And the project API should report Jev as configured without exposing the key

    Scenario: Testing the connection reports success
      Given the project has Jev credentials saved
      And the provider accepts the credentials
      When the user clicks "Test connection"
      Then "Connection successful" should be shown

    Scenario: Testing the connection reports failure
      Given the project has Jev credentials saved
      And the provider rejects the credentials
      When the user clicks "Test connection"
      Then a "Couldn't connect" message should be shown

    Scenario: Clearing the key disables Jev again
      Given the project has Jev credentials saved
      When the key is cleared
      Then the credentials badge should read "Not configured"

  @authenticated
  Rule: Feature settings

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_JEV_..." exists with Jev credentials saved

    Scenario: Excluding fields and narrowing auto-assign scope persists
      When the user turns off "Importance" in "Fields Jev can set"
      And selects "Human members only" under "Who Jev can assign to"
      And clicks "Save changes"
      Then "Saved ✓" should be shown
      And the project's settings should store the exclusion and the scope
      And after reloading, "Save changes" should be disabled (nothing unsaved)

    Scenario: Turning auto-fill off hides the field list and persists
      When the user switches off "Task Auto-fill" and saves
      Then the "Fields Jev can set" list should be hidden
      And the project's settings should store auto-fill as disabled

    Scenario: The Jev Condition node is offered once Jev is configured
      Given an automation exists in the project
      When the user adds a "Jev Condition" node from the "Add Condition" menu
      Then the node's config panel should ask for an answer type and a question for Jev
