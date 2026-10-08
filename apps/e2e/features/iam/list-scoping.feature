@iam @list-scoping
Feature: Lists, pages and counts under a scoped role
  A route gate on a project cannot hide single rows, so list endpoints apply the
  caller's scope inside the SQL query, before pagination and counts. A member
  limited to one sprint's tasks therefore sees consistent pages and totals, and
  the workspace home (open-task count, "assigned to me") only counts and lists
  the tasks they may read.

  Rule: Pages and counts under a scoped role

    Background:
      Given the user already has a stored authenticated admin session
      And a project with five tasks in sprint S1, four in S2 and three in the backlog

    Scenario: Every page and the total count only the tasks of the sprint
      Given a member whose only task access is the tasks of S1
      When the member walks the task list two items at a time
      Then there are three pages (2 + 2 + 1) and the total is 5 on every page
      And only S1's tasks appear, and a page size over 5 returns all of them with no cursor

    Scenario: The admin and a scoped member each see their own totals for the same project
      Then the admin and a Viewer member see a total of 12
      And a member limited to S2 sees a total of 4 and its four tasks

    Scenario: Two scoped roles on one member add up their sprints
      Given a member holding one role for S1 and another for S2
      Then the member's total is 9 and no backlog task is listed

    Scenario: Filters and search narrow the scope further, never widen it
      Given a member limited to S1
      Then a search matching tasks in every sprint returns only the S1 task
      And a sprint filter naming S1 and S2 still returns only S1's five tasks

  Rule: The workspace home respects a scoped role

    Background:
      Given the user already has a stored authenticated admin session
      And a member limited to S1 is assigned two tasks in S1 and one in S2
      And the project has one unassigned task in each sprint

    Scenario: The open-task count only counts tasks the member may read
      Then the member's workspace open-task count is 3
      When one of their tasks is moved to a done status
      Then the count is 2

    Scenario: 'Assigned to me' lists only assigned tasks inside the member's scope
      Then the member's assigned tasks are the two S1 tasks, followed across pages

    Scenario: The home page shows the scoped open-task count and assigned tasks
      When the member signs in and opens the home page
      Then the "Open Tasks" card shows 3
      And the two S1 tasks are listed and the S2 task is not
