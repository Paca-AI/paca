@ui @global-search
Feature: Global search
  The sidebar search (and Cmd/Ctrl+K) finds projects, tasks and people across
  the workspace. It only returns what the caller may see: projects they may
  read, the tasks their roles let them read in each project, and the members
  of projects whose member list they may read (their teams). Only a user admin
  (who may manage users) finds people outside their teams; the default role's
  users:read does not.

  Rule: Results follow the caller's access

    Background:
      Given the user already has a stored authenticated admin session
      And project A has a task in sprint S1 and a task in the backlog
      And project B has a task
      And a teammate is a member of project A and an outsider only of project B

    Scenario: A member limited to one sprint finds only what they may read
      Given a member of project A who may read its members and only S1's tasks
      When the member searches for the run's token
      Then they find project A but not project B
      And they find only the S1 task
      And they find the teammate but not the outsider

    Scenario: A member who may not read the member list finds no people
      Given a member of project A who may read its tasks but not its members
      When the member searches for the run's token
      Then they find both of project A's tasks and no people

    Scenario: A user with no projects finds nothing
      Given a user who is a member of no project
      When the user searches for the run's token
      Then every result list is empty

    Scenario: The admin finds every match
      When the admin searches for the run's token
      Then they find both projects, all three tasks and both people

    Scenario: A task key finds that task first
      When the admin searches for project A's key and the S1 task's number
      Then the S1 task is the first task result

  Rule: The search dialog

    Background:
      Given the user already has a stored authenticated admin session
      And project A has a task in sprint S1

    Scenario: The sidebar button opens the search
      Given the admin is on the home page
      When the admin clicks Search in the sidebar
      Then the search input is shown

    Scenario: Cmd/Ctrl+K opens the search and Enter opens the first task
      Given the admin is on the home page
      When the admin presses Cmd/Ctrl+K and types the S1 task's title
      Then the task is listed with its key
      And pressing Enter opens the task's page
