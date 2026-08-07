@projects @agents
Feature: AI agent management
  Agents are the AI teammates a project (or, for workspace-wide agents, the
  whole instance) can assign work to and chat with. A project's Agents page
  (routes/_authenticated/projects/$projectId/agents/) lists only agents
  owned by that project (project_id set) — agents invited in as members from
  the global roster are managed elsewhere and do not appear here. The
  Admin > Agents page (routes/_authenticated/admin/agents/) is the same
  card grid, empty state, and two-step creation wizard reused for
  project-less "global" agents, gated by the agents.read/agents.write
  global permissions instead of a project permission. Every agent is either
  an "llm" agent (a provider/model/API key/base URL/system prompt the
  server calls directly) or an "acp" agent (a locally run CLI — Claude
  Code, Codex, Gemini CLI, or a custom command — bridged in over a
  generated token). Only an ACP agent shows the "Local Bridge" setup flow;
  an LLM agent has nothing to bridge.

  @authenticated
  Rule: Project Agents page — loading, empty state, and permission-gated actions

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_AGENTS_PROJECT" exists
      And the user has navigated to the "E2E_AGENTS_PROJECT" project

    Scenario: The Agents page shows a loading skeleton while agents are being fetched
      When the user navigates to the Agents page for "E2E_AGENTS_PROJECT"
      Then the Agents page should briefly display loading skeleton cards

    Scenario: A project with no agents shows an empty state
      Given "E2E_AGENTS_PROJECT" has no agents
      When the user navigates to the Agents page for "E2E_AGENTS_PROJECT"
      Then the Agents page should display an empty state with a "Create your first agent" action

    Scenario: The "New agent" button is visible with agents.write permission
      Given the user has the "agents.write" project permission in "E2E_AGENTS_PROJECT"
      When the user navigates to the Agents page for "E2E_AGENTS_PROJECT"
      Then the "New agent" button should be visible

    Scenario: The "New agent" button is hidden without agents.write permission
      Given the user does not have the "agents.write" project permission in "E2E_AGENTS_PROJECT"
      When the user navigates to the Agents page for "E2E_AGENTS_PROJECT"
      Then the "New agent" button should not be visible

    Scenario: The per-card configure/delete menu is hidden without agents.write permission
      Given the user does not have the "agents.write" project permission in "E2E_AGENTS_PROJECT"
      And "E2E_AGENTS_PROJECT" has an agent named "E2E_AGENTS_READONLY_BOT"
      When the user navigates to the Agents page for "E2E_AGENTS_PROJECT"
      Then the card for "E2E_AGENTS_READONLY_BOT" should not show a configure/delete menu

    Scenario: An agent card shows its name, handle, and provider badge
      Given "E2E_AGENTS_PROJECT" has an LLM agent named "E2E_AGENTS_CARD_BOT" using provider "anthropic" and model "claude-sonnet-4-6"
      When the user navigates to the Agents page for "E2E_AGENTS_PROJECT"
      Then the card for "E2E_AGENTS_CARD_BOT" should display the handle "@e2e-agents-card-bot"
      And the card for "E2E_AGENTS_CARD_BOT" should display the "anthropic" provider badge
      And the card for "E2E_AGENTS_CARD_BOT" should display the model "anthropic/claude-sonnet-4-6"

    Scenario: An ACP agent card shows a connection status dot instead of a model
      Given "E2E_AGENTS_PROJECT" has an ACP agent named "E2E_AGENTS_ACP_BOT" using provider "claude-code"
      When the user navigates to the Agents page for "E2E_AGENTS_PROJECT"
      Then the card for "E2E_AGENTS_ACP_BOT" should display the "claude-code" provider badge
      And the card for "E2E_AGENTS_ACP_BOT" should display a bridge status of "Disconnected"

  @authenticated
  Rule: Creating an LLM-type agent

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_AGENTS_CREATE_PROJECT" exists
      And the user has the "agents.write" project permission in "E2E_AGENTS_CREATE_PROJECT"
      And the project has at least one project role
      And the user has navigated to the Agents page for "E2E_AGENTS_CREATE_PROJECT"

    Scenario: The create dialog opens on step 1 with the LLM type selected by default
      When the user clicks the "New agent" button
      Then the create agent dialog should open on step 1
      And the "LLM" agent type option should be selected by default
      And the preset grid should be visible

    Scenario: Step 1 requires a name, a handle, and a project role before continuing
      When the user clicks the "New agent" button
      Then the "Continue" button should be disabled
      When the user fills the agent name with "E2E_AGENTS_NEW_BOT"
      Then the handle field should be auto-filled with "e2e-agents-new-bot"
      And the "Continue" button should still be disabled
      When the user selects a project role for the new agent
      Then the "Continue" button should be enabled

    Scenario: Selecting a preset pre-fills the provider, model, and system prompt
      When the user clicks the "New agent" button
      And the user selects the "Code Reviewer" preset
      And the user fills the agent name with "E2E_AGENTS_PRESET_BOT"
      And the user selects a project role for the new agent
      And the user clicks "Continue"
      Then the provider select should default to "anthropic"
      And the system prompt field should be pre-filled with the "Code Reviewer" preset prompt

    Scenario: Step 2 requires a provider, model, base URL, and API key before creating an LLM agent
      When the user clicks the "New agent" button
      And the user fills the agent name with "E2E_AGENTS_LLM_BOT"
      And the user selects a project role for the new agent
      And the user clicks "Continue"
      Then the "Create agent" button should be disabled
      When the user fills in the LLM API key
      Then the "Create agent" button should be enabled

    Scenario: Creating a valid LLM agent adds it to the list and closes the dialog
      When the user clicks the "New agent" button
      And the user fills the agent name with "E2E_AGENTS_LLM_CREATED"
      And the user selects a project role for the new agent
      And the user clicks "Continue"
      And the user fills in the LLM API key
      And the user clicks "Create agent"
      Then the create agent dialog should close
      And the Agents page should list "E2E_AGENTS_LLM_CREATED"

    Scenario: Cancelling step 1 discards the in-progress agent
      When the user clicks the "New agent" button
      And the user fills the agent name with "E2E_AGENTS_CANCELLED"
      And the user clicks "Cancel"
      Then the create agent dialog should close
      And the Agents page should not list "E2E_AGENTS_CANCELLED"

  @authenticated
  Rule: Creating an ACP-type agent and setting up its local bridge

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_AGENTS_ACP_PROJECT" exists
      And the user has the "agents.write" project permission in "E2E_AGENTS_ACP_PROJECT"
      And the project has at least one project role
      And the user has navigated to the Agents page for "E2E_AGENTS_ACP_PROJECT"

    Scenario: Switching to the ACP agent type hides the LLM-only fields
      When the user clicks the "New agent" button
      And the user selects the "ACP" agent type
      Then the preset grid should be hidden
      And the system prompt field should be hidden

    Scenario: The custom ACP provider requires a shell command before continuing
      When the user clicks the "New agent" button
      And the user selects the "ACP" agent type
      And the user fills the agent name with "E2E_AGENTS_CUSTOM_ACP"
      And the user selects a project role for the new agent
      And the user clicks "Continue"
      And the user selects the "Custom command" ACP provider
      Then the "Create agent" button should be disabled
      When the user fills in a custom ACP command
      Then the "Create agent" button should be enabled

    Scenario: Creating an ACP agent opens the Local Bridge setup dialog automatically
      When the user clicks the "New agent" button
      And the user selects the "ACP" agent type
      And the user fills the agent name with "E2E_AGENTS_ACP_CREATED"
      And the user selects a project role for the new agent
      And the user clicks "Continue"
      And the user clicks "Create agent"
      Then the create agent dialog should close
      And the "Local Bridge" setup dialog should open for "E2E_AGENTS_ACP_CREATED"

    Scenario: The Local Bridge setup dialog walks through install, run, skill, and MCP steps
      Given "E2E_AGENTS_ACP_PROJECT" has an ACP agent named "E2E_AGENTS_BRIDGE_BOT" using provider "claude-code"
      When the user opens the "Local Bridge" setup dialog for "E2E_AGENTS_BRIDGE_BOT"
      Then the dialog should show step 1 "Install the bridge" with a copyable install command
      And the dialog should show step 2 "Run the bridge"
      And the dialog should show step 3 "Install the Paca skill"
      And the dialog should show step 4 "Connect the MCP server"

    Scenario: Generating a bridge token reveals a one-time run command
      Given "E2E_AGENTS_ACP_PROJECT" has an ACP agent named "E2E_AGENTS_TOKEN_BOT" using provider "claude-code"
      And the agent has no bridge token yet
      When the user opens the "Local Bridge" setup dialog for "E2E_AGENTS_TOKEN_BOT"
      And the user clicks "Generate token"
      Then a one-time token warning should be displayed
      And a copyable run command containing the generated token should be displayed
      And the connection status dot should still read "Disconnected" until the bridge connects

    Scenario: Generate token is disabled without agents.write permission
      Given the user does not have the "agents.write" project permission in "E2E_AGENTS_ACP_PROJECT"
      And "E2E_AGENTS_ACP_PROJECT" has an ACP agent named "E2E_AGENTS_LOCKED_BOT" using provider "claude-code"
      When the user opens the "Local Bridge" setup dialog for "E2E_AGENTS_LOCKED_BOT"
      Then the "Generate token" button should be disabled

  @authenticated
  Rule: Deleting a project agent

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_AGENTS_DELETE_PROJECT" exists
      And the user has the "agents.write" project permission in "E2E_AGENTS_DELETE_PROJECT"
      And "E2E_AGENTS_DELETE_PROJECT" has an agent named "E2E_AGENTS_TO_DELETE"
      And the user has navigated to the Agents page for "E2E_AGENTS_DELETE_PROJECT"

    Scenario: Deleting an agent asks for confirmation before removing it
      When the user opens the configure/delete menu for "E2E_AGENTS_TO_DELETE"
      And the user clicks "Delete"
      Then a delete confirmation dialog should appear naming "E2E_AGENTS_TO_DELETE"
      When the user confirms the deletion
      Then "E2E_AGENTS_TO_DELETE" should no longer appear on the Agents page

    Scenario: Cancelling the delete confirmation keeps the agent
      When the user opens the configure/delete menu for "E2E_AGENTS_TO_DELETE"
      And the user clicks "Delete"
      And the user cancels the delete confirmation
      Then "E2E_AGENTS_TO_DELETE" should still appear on the Agents page

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
      And the "New agent" button should not be visible

    Scenario: A user with agents.write sees an empty state and can open the create dialog
      Given the user has the "agents.write" global permission
      And there are no global agents
      When the user navigates to the Admin Agents page
      Then the Admin Agents page should display an empty state with a "Create agent" action
      When the user clicks the "Create agent" empty-state button
      Then the create agent dialog should open on step 1
      And the role selector should offer a "No global role" option

    Scenario: Visiting Admin Agents with ?create=true opens the create dialog immediately
      Given the user has the "agents.write" global permission
      When the user navigates to the Admin Agents page with "create=true" in the URL
      Then the create agent dialog should already be open
      When the user closes the create agent dialog
      Then the URL should no longer contain "create=true"

    Scenario: A global LLM agent appears in the grid after creation
      Given the user has the "agents.write" global permission
      When the user navigates to the Admin Agents page
      And the user clicks the "New agent" button
      And the user fills the agent name with "E2E_GLOBAL_LLM_BOT"
      And the user clicks "Continue"
      And the user fills in the LLM API key
      And the user clicks "Create agent"
      Then the Admin Agents page should list "E2E_GLOBAL_LLM_BOT"

    Scenario: Deleting a global agent shows the global-scoped confirmation copy
      Given the user has the "agents.write" global permission
      And there is a global agent named "E2E_GLOBAL_TO_DELETE"
      When the user navigates to the Admin Agents page
      And the user opens the configure/delete menu for "E2E_GLOBAL_TO_DELETE"
      And the user clicks "Delete"
      Then the delete confirmation dialog should show the global-agent deletion description
      When the user confirms the deletion
      Then "E2E_GLOBAL_TO_DELETE" should no longer appear on the Admin Agents page

    Scenario: A global agent card navigates to /admin/agents/:agentId, not the project route
      Given the user has the "agents.write" global permission
      And there is a global agent named "E2E_GLOBAL_NAV_BOT"
      When the user navigates to the Admin Agents page
      And the user clicks the card for "E2E_GLOBAL_NAV_BOT"
      Then the page URL should be the admin agent detail URL for "E2E_GLOBAL_NAV_BOT"
