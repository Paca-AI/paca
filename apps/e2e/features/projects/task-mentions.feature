@projects @notifications
Feature: Mentions that say where
  A mention notification names the project, the sprint (or Backlog), the task
  and its current status, and shows the start of the text that mentioned you
  with your @mention highlighted. Clicking it opens the task scrolled to that
  comment. The same cues appear in context: a badge on the task row (and a count
  on sprint headers in a list grouped by sprint), an "unread since" divider in the activity pane, and a
  full /notifications page with filters.

  Rule: The mention card

    Background:
      Given a project with a sprint, a task in that sprint, and a member who can edit tasks
      And the admin is signed in as the commenter

    Scenario: A comment mention carries its context over the API
      When the admin comments on the task mentioning the member
      Then the member's notification has the comment id, source "comment", an excerpt with "@<member>", the sprint name and the task's status

    Scenario: A task in the backlog has no sprint
      When the admin mentions the member on a backlog task
      Then the notification's sprint_id is null

    Scenario: The /notifications page shows the card
      When the member opens /notifications
      Then the card shows the project, the sprint, "#<n> <title>", the status and the excerpt
      And "@<member>" in the excerpt is highlighted

    Scenario: Several mentions on one task collapse into one card
      When the admin mentions the member twice on the task
      Then the page shows one card "2 mentions in #<n> — <title>"

    Scenario: Clicking the card opens the task at the comment
      When the member clicks the card
      Then the URL has "?comment=<id>"
      And that comment is highlighted in the activity pane
      And the notification is marked read

  Rule: Mentions in a description and in a document

    Scenario: A description mention creates an in-app row
      When the admin edits the task description to mention the member
      Then the member has a notification with source "description" and no comment id

  Rule: Filters

    Scenario: The Mentions and Assigned tabs filter by type
      Given the member has a mention and an assignment
      When the member opens /notifications and picks "Mentions"
      Then only the mention is listed
      When the member picks "Assigned"
      Then only the assignment is listed

    Scenario: The project filter narrows the list
      When the member picks another project in the project filter
      Then the list is empty

  Rule: Unread cues

    # The sprint-header count only exists in a list grouped by sprint
    # (list-view.tsx); it is covered by no e2e scenario yet.
    Scenario: The task row shows an unread-mention badge
      When the member opens the sprint page
      Then the task row has a mention badge

    Scenario: Opening the task reads the mention and clears the badges
      When the member opens the task
      Then the task row's badge is gone after returning to the list

    Scenario: New activity since the last visit gets a divider
      Given the member opened the task once
      When the admin comments and the member opens the task again
      Then "New since your last visit" is shown above the admin's comment
      And it is not shown on the member's first visit

  Rule: Arriving while online

    Scenario: A toast announces a mention
      Given the member has the app open
      When the admin mentions the member
      Then a toast shows the actor, task, project and excerpt with an "Open" button
