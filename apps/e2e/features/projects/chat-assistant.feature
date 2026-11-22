@projects @chat @calls @assistant
Feature: Meeting assistant
  A meeting agent joins a call when someone adds it, listens to each person,
  shows live captions and proposes tasks that the person who asked approves on
  their own screen. The e2e stack must run the meeting-agent worker, and the
  agent's transcription and model endpoints must be reachable from it; the
  spec starts a fake for both. It skips itself when assistants are off.

  Background:
    Given a project with a voice channel, a meeting agent set up to use the fake endpoints, and an admin signed in

  Scenario: The assistant listens, suggests a task and the person creates it
    When the admin joins the call and adds the assistant
    Then the call says the assistant is listening and transcribing and what it keeps
    And live captions of what was said appear
    When the admin asks for suggestions
    Then a task suggestion card appears with the suggested title
    When the admin edits the title and creates the task
    Then the task exists with the edited title and the card is gone
    When the admin leaves the call
    Then the assistant leaves too

  Scenario: Someone who cannot invite agents is offered no way to add one
    Given a member who may join calls but not invite agents
    When the member joins the call
    Then there is no Add assistant button
