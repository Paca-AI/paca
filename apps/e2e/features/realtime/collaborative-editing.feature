Feature: Collaborative editing
  People editing the same doc or task description see each other's changes live

  Scenario: Two editors share a doc
    Given a doc with stored content and two members who can edit docs
    When both open the doc
    Then the stored content appears once for each of them
    And text typed by one appears for the other without a reload
    And the doc is saved with both people's text

  Scenario: Two editors share a task description
    Given a task with a stored description and two members who can edit tasks
    When both open the task
    Then text typed by one appears in the other's description without a reload
