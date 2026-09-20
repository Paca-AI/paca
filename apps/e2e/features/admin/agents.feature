@admin @agents
Feature: Global agent management
  The Admin > Agents page (routes/_authenticated/admin/agents/) manages
  project-less "global" agents — the same card grid, empty state, and
  two-step creation wizard as a project's Agents page (see
  features/projects/agents.feature), gated by the agents.read/agents.write
  global permissions instead of a project permission. A global agent has no
  project role to assign; instead its role picker offers real global roles
  plus an always-present "No role" option.

  @authenticated
  Rule: Admin > Agents — the global agent equivalent

    Background:
      Given the user already has a stored authenticated session

    Scenario: A user without agents.read or agents.write is redirected away from Admin > Agents
      Given the user does not have the "agents.read" global permission
      And the user does not have the "agents.write" global permission
      When the user navigates to the Admin Agents page
      Then the user should be redirected to the home page

    Scenario: A user with agents.read can view the global agents list but not create agents
      Given the user has the "agents.read" global permission
      And the user does not have the "agents.write" global permission
      When the user navigates to the Admin Agents page
      Then the Admin Agents page should be displayed
      And the "New Agent" button should not be visible

    Scenario: A user with only agents.write sees a no-permission notice but can still create agents
      Given the user has the "agents.write" global permission
      And the user does not have the "agents.read" global permission
      When the user navigates to the Admin Agents page
      Then the Admin Agents page should display the "You don't have permission to view agents" notice
      And the "New Agent" button should be visible

    Scenario: A user with agents.read and agents.write sees an empty state and can open the create dialog
      Given the user has the "agents.read" global permission
      And the user has the "agents.write" global permission
      And there are no global agents
      When the user navigates to the Admin Agents page
      Then the Admin Agents page should display an empty state with a "Create agent" action
      When the user clicks the "Create agent" empty-state button
      Then the create agent dialog should open on step 1
      And the role selector should offer a "No role" option

    Scenario: Visiting Admin Agents with ?create=true opens the create dialog immediately
      Given the user has the "agents.write" global permission
      And the user has the "agents.read" global permission
      When the user navigates to the Admin Agents page with "create=true" in the URL
      Then the create agent dialog should already be open
      When the user closes the create agent dialog
      Then the URL should no longer contain "create=true"

    Scenario: A global LLM agent appears in the grid after creation
      Given the user has the "agents.write" global permission
      And the user has the "agents.read" global permission
      When the user navigates to the Admin Agents page
      And the user clicks the "New Agent" button
      And the user fills the agent name with "E2E_GLOBAL_LLM_BOT"
      And the user clicks "Continue"
      And the user fills in the LLM API key
      And the user clicks "Create Agent"
      Then the Admin Agents page should list "E2E_GLOBAL_LLM_BOT"

    Scenario: Deleting a global agent shows the global-scoped confirmation copy
      Given the user has the "agents.write" global permission
      And the user has the "agents.read" global permission
      And there is a global agent named "E2E_GLOBAL_TO_DELETE"
      When the user navigates to the Admin Agents page
      And the user opens the configure/delete menu for "E2E_GLOBAL_TO_DELETE"
      And the user clicks "Delete"
      Then the delete confirmation dialog should show the global-agent deletion description
      When the user confirms the deletion
      Then "E2E_GLOBAL_TO_DELETE" should no longer appear on the Admin Agents page

    Scenario: A global agent card navigates to /admin/agents/:agentId, not the project route
      Given the user has the "agents.write" global permission
      And the user has the "agents.read" global permission
      And there is a global agent named "E2E_GLOBAL_NAV_BOT"
      When the user navigates to the Admin Agents page
      And the user clicks the card for "E2E_GLOBAL_NAV_BOT"
      Then the page URL should be the admin agent detail URL for "E2E_GLOBAL_NAV_BOT"
