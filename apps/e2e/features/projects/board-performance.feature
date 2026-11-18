@projects @interactions @board @performance
Feature: Board stays responsive with many tasks
  A project board renders one card per loaded task. Interacting with the board
  — opening a task, or scrolling a column to load its next page — must only do
  work proportional to what changed, not re-render every card on the board. On
  a slower machine (modelled here with a 4x CPU slowdown) the main thread must
  not be blocked for long when a user opens a task or loads more cards.
  Regression coverage for GitHub issue #781.

  @authenticated
  Rule: Board interactions do not block the main thread

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_PERF_PROJECT" exists with 200 tasks assigned to a member
      And the project has a Board view in the backlog
      And the user is viewing the board with a 4x CPU slowdown

    Scenario: Opening a task does not re-render the whole board
      When the user clicks a task card
      Then the task detail opens
      And the cards on the board are not all re-rendered

    Scenario: Opening a task does not re-render every table row
      Given the backlog is showing its default Table view with 100 or more rows loaded
      When the user clicks a task row
      Then the task detail opens
      And the rows of the table are not all re-rendered

    Scenario: Loading the next page of a column does not redraw the other cards
      When the user scrolls a column to the bottom so its next page loads
      Then the cards that were already on screen keep their DOM nodes
      And the avatars on screen are not fetched again, although each response re-signs their URLs
      And the main thread is not blocked for long while the page loads
