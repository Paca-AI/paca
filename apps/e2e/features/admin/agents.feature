@admin @agents
Feature: Global agent management
  The Admin > Agents page (routes/_authenticated/admin/agents/) manages
  project-less "global" agents — the same card grid, empty state, and
  creation wizard as a project's Agents page (see
  features/projects/agents.feature), gated by the agents.read/agents.write
  global permissions instead of a project permission. A global agent has no
  project role to assign. It starts with the default global role, which the
  server assigns at creation; changing it is its own permission
  (global_roles.assign) and its own request, so creating an agent never carries
  one. The wizard creates the agent at its second step, then offers a third
  "Global role" step (the real global roles plus an always-present "No global
  role" choice) for someone who may assign roles, and stays at two steps for
  anyone else. An agent's role is later shown, changed and removed on its own
  "Global role" tab.

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
      Then the create agent dialog should open on step 1 of 3
      And the first step should not contain a role field

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
      Then the create agent dialog should show step 3 of 3 with the default role "USER" selected
      When the user clicks "Finish"
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

  @authenticated
  Rule: Creating a global agent - its role is a third step, after the agent exists

    Background:
      Given the user already has a stored authenticated admin session

    Scenario: The agent is created first, then the role step shows it holding the default role
      When the user navigates to the Admin Agents page
      And the user clicks the "New Agent" button
      Then the first step should describe setting up the agent's identity
      When the user fills the agent name with "E2E_GLOBAL_ROLE_STEP"
      And the user clicks "Continue"
      And the user fills in the LLM API key
      And the user clicks "Create Agent"
      Then the create agent dialog should show step 3 of 3
      And the dialog should say that "E2E_GLOBAL_ROLE_STEP" was created with the default global role
      And "USER" should be selected and marked as both the current role and the default
      And "No global role" should not be selected
      And the role step should list "ADMIN" and "SUPER_ADMIN"
      And the dialog should not offer "Back"
      And the agent "E2E_GLOBAL_ROLE_STEP" should hold the global role "USER"

    Scenario: Keeping the default role creates the agent with it
      When the user creates a global agent "E2E_GLOBAL_DEFAULT_ROLE" and clicks "Finish" on the role step
      Then the Admin Agents page should list "E2E_GLOBAL_DEFAULT_ROLE"
      And the agent "E2E_GLOBAL_DEFAULT_ROLE" should hold the global role "USER"

    Scenario: A different role is assigned through its own step once the agent exists
      When the user creates a global agent "E2E_GLOBAL_WITH_ROLE" choosing the role "ADMIN" on the role step
      Then the Admin Agents page should list "E2E_GLOBAL_WITH_ROLE"
      And the agent "E2E_GLOBAL_WITH_ROLE" should hold the global role "ADMIN"

    Scenario: Choosing "No global role" removes the default role
      When the user creates a global agent "E2E_GLOBAL_NO_ROLE" choosing "No global role" on the role step
      Then the agent "E2E_GLOBAL_NO_ROLE" should hold no global role

    Scenario: Closing the dialog on the role step finishes with the role the agent has
      When the user creates a global agent "E2E_GLOBAL_CLOSED_ON_ROLE" and closes the dialog on the role step
      Then the Admin Agents page should list "E2E_GLOBAL_CLOSED_ON_ROLE"
      And the agent "E2E_GLOBAL_CLOSED_ON_ROLE" should hold the global role "USER"

    Scenario: A user who may write agents but not assign roles creates them in two steps and they still get the default role
      Given the user has the "agents.write" global permission
      And the user does not have the "global_roles.assign" global permission
      When the user navigates to the Admin Agents page
      And the user clicks the "New Agent" button
      Then the create agent dialog should show step 1 of 2
      When the user fills the agent name with "E2E_GLOBAL_TWO_STEPS"
      And the user clicks "Continue"
      Then the create agent dialog should show step 2 of 2
      And the dialog should offer "Create Agent" rather than "Continue"
      When the user fills in the LLM API key
      And the user clicks "Create Agent"
      Then the agent "E2E_GLOBAL_TWO_STEPS" should hold the global role "USER"

    Scenario: A user who may also assign roles gets the third step
      Given the user has the "agents.write", "global_roles.read" and "global_roles.assign" global permissions
      When the user navigates to the Admin Agents page
      And the user clicks the "New Agent" button
      Then the create agent dialog should show step 1 of 3

  @authenticated
  Rule: A global agent's Global role tab

    Background:
      Given the user already has a stored authenticated admin session
      And there is a global agent named "E2E_GLOBAL_ROLE_TAB"

    Scenario: Changing, removing and assigning the role
      When the user opens the "Global role" tab of "E2E_GLOBAL_ROLE_TAB"
      Then the tab should show the default role "USER" with "Change role" and "Remove role"
      When the user clicks "Change role"
      Then the current role "USER" should be selected and marked as current
      And "Assign role" should be disabled until a role is chosen
      When the user chooses the role "ADMIN" and clicks "Assign role"
      Then the agent should hold the global role "ADMIN"
      When the user clicks "Remove role"
      Then the tab should ask whether to remove the role because the agent will lose the permissions it grants
      When the user confirms with "Remove role"
      Then the tab should say the agent has "No global role"
      And the agent should hold no global role
      When the user clicks "Assign role"
      And the user chooses the role "USER" and clicks "Assign role"
      Then the agent should hold the global role "USER"

    Scenario: A full-access role is flagged before it is assigned
      When the user opens the "Global role" tab of "E2E_GLOBAL_ROLE_TAB"
      And the user clicks "Change role"
      And the user chooses the role "SUPER_ADMIN"
      Then the tab should warn that the agent will be able to do everything
      When the user clicks "Cancel"
      Then the agent should still hold the default role "USER"

    Scenario: Someone who can see the role but not change it gets a read-only tab
      Given the agent "E2E_GLOBAL_ROLE_TAB" holds the global role "ADMIN"
      And the user has the "agents.read" and "global_roles.read" global permissions
      When the user opens the "Global role" tab of "E2E_GLOBAL_ROLE_TAB"
      Then the tab should show the role "ADMIN"
      And the tab should say the user can see the role but not change it
      And the tab should not offer "Change role" or "Remove role"

    Scenario: Writing agents is not enough without global_roles.assign
      Given the user has the "agents.read", "agents.write" and "global_roles.read" global permissions
      And the user does not have the "global_roles.assign" global permission
      When the user opens the "Global role" tab of "E2E_GLOBAL_ROLE_TAB"
      Then the tab should show the default role "USER"
      And the tab should say the user cannot change the role
      And the tab should not offer "Assign role"
