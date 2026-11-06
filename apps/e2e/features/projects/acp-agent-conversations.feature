@projects @agents @conversations @acp
Feature: Conversations with an ACP agent
  An ACP agent runs on the user's own machine: a local bridge daemon connects
  to Paca over the /agent-bridge/ws WebSocket, authenticates with the agent's
  bridge token, and receives "start_turn" / "pause_turn" / "stop_turn"
  messages. The bridge reports progress back as "turn_status" messages.

  The composer's stop button on a conversation calls the pause endpoint. The
  API writes no status for a pause, so the conversation only leaves
  "Running" once the bridge reports "finished" after cancelling the turn (an
  ACP conversation has no "Waiting for reply" state). A
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
      When the bridge reports the turn as "finished"
      Then the conversation badge should read "Finished"
      And the "Stop generating" button should no longer be shown

    Scenario: A full Stop is recorded as Stopped and dispatches stop_turn to the bridge
      Given the user has a running chat conversation with the agent open
      When the conversation is stopped through the API (an ACP conversation has no header Stop button)
      Then the bridge should receive a "stop_turn" for that conversation
      And the conversation should be "stopped"
      And the conversation badge should read "Stopped"

  # That the bridge itself reports nothing after a stop (and "finished" after a
  # pause, including when a stop supersedes a pause) is covered by the
  # acp-bridge runner tests, since the fake bridge here plays the daemon.
