@projects @interactions @views
Feature: Calendar views
  A Calendar view lays the tasks of the Sprint, Backlog or Timeline
  interaction on a month, week or day grid, so a team sees what lands on
  which day. It is added from the "Add view" popover like a Board, Table or
  Gantt view. A task shows on every day from its start date to its due date;
  dragging it to another day moves both dates and keeps its length. The mode
  is remembered per view.

  @authenticated
  Rule: A calendar view is created from the "Add view" popover

    Background:
      Given the user is signed in as an administrator
      And a project exists with a task starting and due this month

    Scenario: Creating a calendar view shows this month with the task on its days
      When the user opens the product backlog
      And the user adds a view named "Release calendar" with the "Calendar" layout
      Then the "Release calendar" view tab should be active
      And the grid should be titled with the current month
      And the task should be shown from its start date to its due date

  @authenticated
  Rule: Dragging a task reschedules it

    Background:
      Given the user is signed in as an administrator
      And a project exists with a backlog calendar view and a task starting and due this month

    Scenario: Dropping a task on another day moves both of its dates
      When the user opens the calendar view
      And the user drags the task onto a later day of the month
      Then the task's start and due dates should both move by the same number of days

  @authenticated
  Rule: The mode is remembered

    Background:
      Given the user is signed in as an administrator
      And a project exists with a backlog calendar view

    Scenario: Switching to the week mode survives a reload
      When the user opens the calendar view
      And the user switches the calendar to "Week"
      And the user reloads the page
      Then the "Week" mode should still be selected
      And the grid should show seven days
