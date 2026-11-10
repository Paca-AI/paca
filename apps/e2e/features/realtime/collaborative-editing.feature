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

  Scenario: An agent's write reaches editors who have the doc open
    Given a doc with two paragraphs and two members editing it
    And one member has typed into the first paragraph without it being saved yet
    When an agent saves new text for the second paragraph over the REST API
    Then both editors show the agent's text without a reload
    And the first paragraph still holds what the member typed
    And the doc is saved with the member's text and the agent's text

  Scenario: An agent adds a block next to a block that is being edited
    Given a doc with two paragraphs and two members editing it
    And one member has typed into the first paragraph without it being saved yet
    When an agent saves the doc with a new paragraph in front, without block ids
    Then both editors show the new paragraph exactly once
    And the first paragraph still holds what the member typed

  Scenario: A block edited live keeps the editor's version
    Given a doc with two paragraphs and two members editing it
    And one member has typed into the second paragraph and the other has seen it
    When an agent saves different text for that same paragraph
    Then both editors keep the typed text and do not show the agent's text
    And the doc is saved with the typed text

  Scenario: An agent's write reaches editors of a task description
    Given a task with a stored description and two members who can edit tasks
    When both open the task
    And an agent saves new description text over the REST API
    Then both editors show the agent's text without a reload
