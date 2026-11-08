@projects @agents @conversations @acp
Feature: Following the tail of a conversation
  While the reader is at the newest event, new events are shown as they
  arrive. While they are scrolled up, new events are withheld behind a
  "N new messages" pill. Sending a message, or clicking the pill, jumps back
  to the tail and resumes following.

  Background:
    Given a project named "E2E_FOLLOW" exists
    And the project has an ACP agent connected through a fake bridge
    And a conversation with 40 messages exists

  Scenario: A message sent while scrolled up is shown without a reload
    Given I open the conversation and scroll to the top
    When I send a message from the composer
    Then my message is visible and in the viewport
    And no "N new messages" pill is shown
