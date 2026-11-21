@projects @chat @tasks @agents
Feature: Task-aware chat and agents in channels
  Chat links to the work: tasks and documents are chips that show their live
  title only to people who may open them, a message can become a task, `/task`
  makes one from the composer, messages are searchable from the Inbox, and an
  @mention of a project's agent starts a run of that agent.

  Background:
    Given a project with a public channel and an admin signed in

  Scenario: A task chip shows its live title to someone who can open the task
    Given a message that links a task
    Then the admin sees a chip with the task's current title that opens the task

  Scenario: A person who cannot read the task sees a locked chip and no title
    Given a member who can read the channel but not tasks
    When the member opens the channel
    Then the chip is locked and the task's title appears nowhere on the page

  Scenario: /task creates a task and reports it with a chip
    When the admin sends "/task <title>" in the channel
    Then a task with that title exists
    And the channel shows a message with a chip for it

  Scenario: A message becomes a task from its toolbar
    When the admin chooses "Create task from message" on a message
    Then a task titled from the message exists
    And the message's thread opens with a chip for the task

  Scenario: Messages are found from the Inbox search
    Given a message containing a distinctive phrase
    When the admin searches the Inbox for part of it
    Then the message is listed and opens in its channel
    And a search shorter than three characters asks for more

  Scenario: Mentioning a project's agent starts a run
    Given an agent that is a member of the project
    When the admin mentions the agent in the channel
    Then a conversation of that agent exists in the project
