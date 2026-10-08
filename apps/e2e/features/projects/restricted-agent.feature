@projects @agents @roles
Feature: Restricting an agent with a role
  An agent is not restricted by a setting on the agent. It is restricted by an
  ordinary project role with a Deny statement on the agent's resource, held by
  the member it should not reach:

    { "effect": "Deny",
      "actions": ["agents:*", "conversations:*"],
      "resources": ["project/<projectId>/agent/<agentId>/*"] }

  Deny always wins over what the member's other roles allow, so that member
  cannot see the agent (it is left out of the Agents list) or use it (every
  route on it, including its chat sessions, is refused), while another member
  holding the same roles is unaffected. A member can hold several roles, so the
  Deny role sits next to the one they have. Taking the role away gives the agent
  back. Environments are restricted the same way (see environments.feature).

  @authenticated
  Rule: A member denied an agent by a role

    Background:
      Given the user already has a stored authenticated admin session
      And a project named "E2E_RESTRICT_PROJECT" exists
      And the project has an agent named "E2E_RESTRICT_SECRET_BOT"
      And "E2E_RESTRICT_BLOCKED" and "E2E_RESTRICT_ALLOWED" are members of the project with the "Editor" role

    Scenario: A member denied the agent cannot see or use it, while another member can
      Given "E2E_RESTRICT_BLOCKED" also holds a project role denying "agents:*" and "conversations:*" on the agent
      Then the API should leave the agent out of the Agents list of "E2E_RESTRICT_BLOCKED" and answer 403 on its detail, chat sessions and new chat
      And the API should still list the agent for "E2E_RESTRICT_ALLOWED" and accept a new chat
      When "E2E_RESTRICT_BLOCKED" signs in and opens the Agents page
      Then the page should show "No agents yet"
      When "E2E_RESTRICT_ALLOWED" signs in and opens the Agents page
      Then the page should list "E2E_RESTRICT_SECRET_BOT"

    Scenario: Denying only conversations leaves the agent visible but not usable
      Given "E2E_RESTRICT_BLOCKED" also holds a project role denying "conversations:*" on the agent
      Then the agent should be listed for "E2E_RESTRICT_BLOCKED" and its detail should load
      But its chat sessions and new chats should be refused with 403

    Scenario: Taking the Deny role away gives the agent back
      Given "E2E_RESTRICT_BLOCKED" also holds a project role denying "agents:*" and "conversations:*" on the agent
      When the Deny role is removed from the roles of "E2E_RESTRICT_BLOCKED"
      Then the agent should be listed for "E2E_RESTRICT_BLOCKED" and its chat sessions should load

    Scenario: A Deny role written in the Advanced editor and given to a member restricts the agent
      When the user creates the project role "E2E_RESTRICT_NO_SECRET_BOT" in the Advanced (JSON) editor with a Deny statement on the agent
      Then the role should be listed in the roles table
      When the role is given to "E2E_RESTRICT_ADVANCED"
      Then the Team page should show the role next to the member's other role
      And the agent should be left out for that member and refused on every route
