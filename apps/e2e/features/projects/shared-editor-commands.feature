@projects @chat @tasks @docs
Feature: One editor and one command list for chat and comments
  Chat messages, task comments and document comments use the same rich-text
  box. Its "/" menu lists the same commands everywhere, and `/task` and `/doc`
  create the item and post a chip linking to it.

  Background:
    Given a project with a public channel, a task, a document and an admin signed in

  Scenario: The "/" menu lists the same commands in chat and in a task comment
    When the admin types "/" in the channel box
    Then the menu offers "Create task" and "Create document"
    When the admin types "/" in the task's comment box
    Then the menu offers "Create task" and "Create document"

  Scenario: /doc in chat creates a document and reports it with a chip
    When the admin sends "/doc <title>" in the channel
    Then a document with that title exists
    And the channel shows a message with a chip for it

  Scenario: /task in a task comment creates a task and comments a chip
    When the admin sends "/task <title>" as a comment on the task
    Then a task with that title exists
    And the comment shows a chip for it

  Scenario: /doc in a document comment creates a document and comments a chip
    When the admin sends "/doc <title>" as a comment on the document
    Then a document with that title exists
    And the comment shows a chip for it
