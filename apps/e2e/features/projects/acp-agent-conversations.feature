@projects @agents @conversations @acp
Feature: Conversations with an ACP agent
  An ACP agent runs on the user's own machine: a local bridge daemon connects
  to Paca over the /agent-bridge/ws WebSocket, authenticates with the agent's
  bridge token, and receives "start_turn" / "pause_turn" / "stop_turn"
  messages. The bridge reports progress back as "turn_status" messages.

  The composer's stop button on a conversation calls the pause endpoint. The
  API writes no status for a pause, so the conversation only leaves
  "Running" once the bridge reports "paused" after cancelling the turn. A
  full Stop (the header button) is recorded by the API as "Stopped" on its
  own, and the bridge must not report anything that overwrites it.

  The e2e stack has no real ACP CLI, so these scenarios connect a fake bridge
  (a plain WebSocket client in the spec) that plays the daemon's part.

  Background:
    Given a project named "E2E_ACPCONV" exists
    And the project has an ACP agent with a generated bridge token
    And a fake bridge is connected for that agent

  Rule: Stopping a running ACP turn

    Scenario: Sending a message dispatches start_turn to the connected bridge
      When the user starts a chat with the agent
      Then the bridge should receive a "start_turn" for that conversation
      And the conversation should be "running"

    Scenario: The composer stop button pauses the turn and the conversation leaves Running
      Given the user has a running chat conversation with the agent open
      When the user presses the "Stop generating" button
      Then the bridge should receive a "pause_turn" for that conversation
      When the bridge reports the turn as "paused"
      Then the conversation badge should read "Waiting for reply"
      And the "Stop generating" button should no longer be shown

    Scenario: A full Stop stays Stopped when the bridge unwinds the turn
      Given the user has a running chat conversation with the agent open
      When the conversation is stopped through the API (an ACP conversation has no header Stop button)
      Then the bridge should receive a "stop_turn" for that conversation
      And the conversation should be "stopped"
      And the conversation should remain "stopped" (the bridge reports nothing)
