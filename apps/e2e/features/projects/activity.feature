@projects @activity
Feature: Project activity log
  Every project has an "Activity" page (routes/_authenticated/projects/
  $projectId/activity/) that lists everything recorded in the project's
  shared activities table, newest first, grouped by day. Each entry names the
  actor, describes what happened and links to the entity it is about; an
  entity that has since been deleted is shown struck through and is not a
  link. The list can be narrowed with a search box and a "Filters" popover
  (type, people, source, date range), and it refreshes when realtime events
  arrive. Reading it needs the project.activities.read permission, which also
  decides whether the sidebar shows the "Activity" item.

  @authenticated
  Rule: Reading the activity log

    Background:
      Given the admin is signed in
      And a project named "E2E_ACTIVITY_..." exists with a sprint and a task created over the API

    Scenario: The sidebar links to the activity page
      When the user opens the project and clicks "Activity" in the sidebar
      Then the "Activity" heading should be visible
      And the page should say "Everything that happened in this project"

    Scenario: Entries recorded over the API are listed under "Today"
      When the user opens the activity page
      Then a "Today" group should be visible
      And an entry "created sprint" for the sprint should be listed
      And an entry "created this task" for the task should be listed

    Scenario: An entry links to its entity
      When the user clicks the sprint's name in the "created sprint" entry
      Then the sprint page should open

    Scenario: A deleted entity is struck through and not linked
      Given the sprint has been deleted over the API
      When the user opens the activity page
      Then an entry "deleted sprint" should be listed
      And the sprint name should not be a link

    Scenario: New activity appears without reloading
      Given the user has the activity page open
      When a sprint is created over the API
      Then its "created sprint" entry should appear

  @authenticated
  Rule: Filtering

    Scenario: Filtering by type hides other entity types
      When the user opens "Filters" and ticks "Sprints"
      Then the sprint entry should be visible
      And the task entry should not be visible
      And the "Filters" button should show a count of 1

    Scenario: Searching narrows the list
      When the user types the task's title into "Search activity…"
      Then the task entry should be visible
      And the sprint entry should not be visible

    Scenario: A search with no match shows the filtered empty state
      When the user searches for text no entry contains
      Then "No matching activity" should be visible
      When the user clicks "Clear all"
      Then the entries should be listed again

  @authenticated
  Rule: Permission

    Scenario: A member without project.activities.read cannot see the log
      Given a member whose role only grants "tasks.read"
      When the member opens the project
      Then the sidebar should not show "Activity"
      When the member opens the activity page directly
      Then "You can't view the activity log" should be visible

    Scenario: A member with project.activities.read can see the log
      Given a member whose role grants "project.activities.read"
      When the member opens the project
      Then the sidebar should show "Activity"
      And the activity page should list the sprint entry
