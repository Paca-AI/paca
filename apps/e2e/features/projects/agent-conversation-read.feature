@projects @agents @conversations @acp
Feature: An agent reads conversations through the agent API
  The read_conversation MCP tool calls GET /agents/me/conversations/:id with
  the agent's own API key. The router gates it on conversations:read, judged
  on the roles of the agent the key proves and over the conversation's own
  resource (project/<P>/conversation/<C>); nothing else the client sends
  counts, so an agent run by the ACP bridge needs no extra header. An IAM
  condition such as conversation.agent_id can limit a role to some
  conversations, for instance the agent's own.

  An owner-private chat belongs to the member who owns it, so another agent can
  never read it. Task-assigned runs are visible to the whole project.

  Background:
    Given a project named "E2E_AGENTREAD" exists
    And a project role without conversations:read and one with it exist
    And each ACP agent has an API key and a connected fake bridge

  Rule: conversations:read is required, for the agent's own conversations too

    Scenario: An agent without conversations:read is refused
      Given an agent without conversations:read has a chat
      When it reads that conversation
      Then the response is 403

    Scenario: An agent with conversations:read reads its own conversation
      Given an agent with conversations:read has a chat
      When it reads that conversation
      Then the response is 200

  Rule: Another agent's conversations

    Scenario: An owner-private chat is not found even with conversations:read
      Given an agent with conversations:read
      And a peer agent has a chat
      When the agent reads the peer's chat
      Then the response is 404

    Scenario: A task-assigned run is readable only with conversations:read
      Given a task is assigned to a peer agent and its bridge starts the run
      When an agent with conversations:read reads the run
      Then the response is 200
      When an agent without conversations:read reads the run
      Then the response is 403

  Rule: A condition can limit an agent to its own conversations

    Scenario: conversation.agent_id equal to the agent's own id
      Given the agent's role allows conversations:read only where conversation.agent_id is its id
      When it reads its own chat
      Then the response is 200
      When it reads a peer's chat
      Then the response is 403
