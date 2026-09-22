@projects @agents
Feature: AI agent management
  Agents are the AI teammates a project can assign work to and chat with. A
  project's Agents page (routes/_authenticated/projects/$projectId/agents/)
  lists only agents owned by that project (project_id set) — agents invited
  in as members from the global roster are managed elsewhere and do not
  appear here. Every agent is either an "llm" agent (a provider/model/API
  key/base URL/system prompt the server calls directly), a "provider_cli"
  agent (a locally-authenticated CLI running in a static project
  environment), or an "acp" agent (a locally run CLI — Claude Code, Codex,
  Gemini CLI, or a custom command — bridged in over a generated token). Only
  an ACP agent shows the "Local Bridge" setup flow; an LLM agent has nothing
  to bridge. The Admin > Agents page (routes/_authenticated/admin/agents/)
  is the same card grid, empty state, and creation wizard reused for
  project-less "global" agents — see features/admin/agents.feature for that
  surface.

  Creating an agent is a three-step wizard: 1 Identity, 2 AI configuration,
  3 Role. A project agent's role is its project role: required, and part of
  the create request, so the last step's "Create Agent" button stays disabled
  until one is chosen and nothing is created before it.

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

    Scenario: The "New Agent" button is visible with agents.write and project.members.write permissions
      Given the user has the "agents.write" project permission in "E2E_AGENTS_PROJECT"
      And the user has the "project.members.write" project permission in "E2E_AGENTS_PROJECT"
      When the user navigates to the Agents page for "E2E_AGENTS_PROJECT"
      Then the "New Agent" button should be visible

    Scenario: The "New Agent" button is hidden without agents.write permission
      Given the user does not have the "agents.write" project permission in "E2E_AGENTS_PROJECT"
      When the user navigates to the Agents page for "E2E_AGENTS_PROJECT"
      Then the "New Agent" button should not be visible

    Scenario: The "New Agent" button stays hidden with agents.write but no project.members.write
      Given the user has the "agents.write" project permission in "E2E_AGENTS_PROJECT"
      And the user does not have the "project.members.write" project permission in "E2E_AGENTS_PROJECT"
      When the user navigates to the Agents page for "E2E_AGENTS_PROJECT"
      Then the "New Agent" button should not be visible

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
      And the card for "E2E_AGENTS_ACP_BOT" should display a bridge status of "Offline"

  @authenticated
  Rule: Creating an LLM-type agent

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_AGENTS_CREATE_PROJECT" exists
      And the user has the "agents.write" project permission in "E2E_AGENTS_CREATE_PROJECT"
      And the user has the "project.members.write" project permission in "E2E_AGENTS_CREATE_PROJECT"
      And the project has at least one project role
      And the user has navigated to the Agents page for "E2E_AGENTS_CREATE_PROJECT"

    Scenario: The create dialog opens on step 1 of 3 with the LLM type selected by default
      When the user clicks the "New Agent" button
      Then the create agent dialog should open on step 1 of 3
      And the "LLM (API key)" agent type option should be selected by default
      And the preset grid should be visible

    Scenario: Step 1 requires a name and a handle before continuing, and does not ask for a role
      When the user clicks the "New Agent" button
      Then the "Continue" button should be disabled
      When the user fills the agent name with "E2E_AGENTS_NEW_BOT"
      Then the handle field should be auto-filled with "e2e-agents-new-bot"
      And the "Continue" button should be enabled
      And the first step should not contain a project role field

    Scenario: Selecting a preset pre-fills the provider, model, and system prompt
      When the user clicks the "New Agent" button
      And the user selects the "Code Reviewer" preset
      And the user fills the agent name with "E2E_AGENTS_PRESET_BOT"
      And the user clicks "Continue"
      Then the provider select should default to "anthropic"
      And the system prompt field should be pre-filled with the "Code Reviewer" preset prompt

    Scenario: Step 2 requires a provider, model, base URL, and API key before continuing to the role
      When the user clicks the "New Agent" button
      And the user fills the agent name with "E2E_AGENTS_LLM_BOT"
      And the user clicks "Continue"
      Then the "Continue" button should be disabled
      And there should be no "Create Agent" button yet
      When the user fills in the LLM API key
      Then the "Continue" button should be enabled

    Scenario: Step 3 asks for the project role, and the agent cannot be created without one
      When the user clicks the "New Agent" button
      And the user fills the agent name with "E2E_AGENTS_ROLE_STEP"
      And the user clicks "Continue"
      And the user fills in the LLM API key
      And the user clicks "Continue"
      Then the create agent dialog should show step 3 of 3
      And the dialog should say to choose the agent's role in this project
      And every project role should be offered, none of them preselected
      And there should be no "No global role" choice
      And the "Create Agent" button should be disabled
      When the user selects a project role for the new agent
      Then the "Create Agent" button should be enabled
      When the user clicks "Back"
      Then the create agent dialog should show step 2 of 3

    Scenario: Creating a valid LLM agent adds it to the list and closes the dialog
      When the user clicks the "New Agent" button
      And the user fills the agent name with "E2E_AGENTS_LLM_CREATED"
      And the user clicks "Continue"
      And the user fills in the LLM API key
      And the user clicks "Continue"
      And the user selects a project role for the new agent
      And the user clicks "Create Agent"
      Then the create agent dialog should close
      And the Agents page should list "E2E_AGENTS_LLM_CREATED"

    Scenario: The project agent joins the project with the role chosen in step 3
      When the user clicks the "New Agent" button
      And the user fills the agent name with "E2E_AGENTS_ROLE_CHOSEN"
      And the user clicks "Continue"
      And the user fills in the LLM API key
      And the user clicks "Continue"
      And the user chooses the project role "Viewer"
      And the user clicks "Create Agent"
      Then the create agent dialog should close
      And the Agents page should list "E2E_AGENTS_ROLE_CHOSEN"
      And the agent "E2E_AGENTS_ROLE_CHOSEN" should be a member of the project with the role "Viewer"

    Scenario: Cancelling step 1 discards the in-progress agent
      When the user clicks the "New Agent" button
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
      When the user clicks the "New Agent" button
      And the user selects the "ACP (local CLI)" agent type
      Then the preset grid should be hidden
      When the user fills the agent name with "E2E_AGENTS_ACP_FIELDS"
      And the user clicks "Continue"
      Then step 2 should show the "ACP Server" section
      And the system prompt field should be hidden

    Scenario: The custom ACP provider requires a shell command before continuing
      When the user clicks the "New Agent" button
      And the user selects the "ACP (local CLI)" agent type
      And the user fills the agent name with "E2E_AGENTS_CUSTOM_ACP"
      And the user clicks "Continue"
      And the user selects the "Custom…" ACP provider
      Then the "Continue" button should be disabled
      When the user fills in a custom ACP command
      Then the "Continue" button should be enabled

    Scenario: Creating an ACP agent opens the bridge setup dialog with a token already generated
      When the user clicks the "New Agent" button
      And the user selects the "ACP (local CLI)" agent type
      And the user fills the agent name with "E2E_AGENTS_ACP_CREATED"
      And the user clicks "Continue"
      And the user clicks "Continue"
      And the user selects a project role for the new agent
      And the user clicks "Create Agent"
      Then the create agent dialog should close
      And the "Connect your local ACP bridge" dialog should open for "E2E_AGENTS_ACP_CREATED"
      And the dialog should show step 1 "Install the ACP bridge"
      And the dialog should show step 2 "Install the skill & connect the MCP server"
      And the dialog should show step 3 "Run the local bridge"
      And a one-time token warning and a copyable run command should already be displayed
      And the connection status should read "Not connected"
      When the user clicks "Done"
      Then the setup dialog should close
      And the Agents page should list "E2E_AGENTS_ACP_CREATED"

    Scenario: The Local Bridge panel on an ACP agent's detail page walks through install, skill/MCP, and run steps
      Given "E2E_AGENTS_ACP_PROJECT" has an ACP agent named "E2E_AGENTS_BRIDGE_BOT" using provider "claude-code"
      When the user opens the detail page of "E2E_AGENTS_BRIDGE_BOT"
      Then the "Local Bridge" panel should show step 1 "Install the ACP bridge" with a copyable install command
      And the panel should show step 2 "Install the skill & connect the MCP server"
      And the panel should show step 3 "Run the local bridge"

    Scenario: Generating a bridge token reveals a one-time run command
      Given "E2E_AGENTS_ACP_PROJECT" has an ACP agent named "E2E_AGENTS_TOKEN_BOT" using provider "claude-code"
      And the agent has no bridge token yet
      When the user opens the detail page of "E2E_AGENTS_TOKEN_BOT"
      Then the panel should prompt the user to generate a token to reveal the run command
      When the user clicks "Generate token"
      Then a one-time token warning should be displayed
      And a copyable run command containing the generated token should be displayed
      And the connection status should still read "Not connected" until the bridge connects

    Scenario: Generate token is disabled without agents.write permission
      Given the user has only the "agents.read" project permission in "E2E_AGENTS_ACP_PROJECT"
      And "E2E_AGENTS_ACP_PROJECT" has an ACP agent named "E2E_AGENTS_LOCKED_BOT" using provider "claude-code"
      When the user opens the detail page of "E2E_AGENTS_LOCKED_BOT"
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
