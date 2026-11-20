@projects @presence
Feature: Who is viewing and typing on a task
  Opening a task joins a live room for it (services/realtime/src/presence.ts).
  Other people looking at the same task appear as avatars in the task header,
  and "<name> is typing…" shows above the comment composer while somebody
  writes a comment. Both are ephemeral: nothing is stored, a typing lease
  lapses after a few seconds, and access is checked once when joining.

  Rule: Viewers and typing

    Background:
      Given a project with a task, the admin, and a member who can edit tasks

    Scenario: A second person opening the task appears as a viewer
      When both open the task
      Then each sees the other in the task header's viewers
      And neither sees themselves

    Scenario: Typing a comment shows a typing indicator to the other person
      When the member types in the comment composer
      Then the admin sees "<member> is typing…"
      And the indicator disappears when the member clears the composer

    Scenario: A member who turned typing broadcast off is not announced
      Given the member switched "Show when I am typing a comment" off
      When the member types in the comment composer
      Then the admin never sees a typing indicator
      But the admin still sees the member as a viewer

    Scenario: Leaving the task removes the viewer
      When the member navigates away from the task
      Then the admin no longer sees the member as a viewer

  Rule: Access is checked at join

    Scenario: A read-only member is listed as a viewer but cannot send typing
      Given a member who can only read tasks has the task open
      Then the admin sees them as a viewer
      When that member sends a typing message over the socket
      Then the admin sees no typing indicator

    Scenario: A user who cannot read the task cannot join its live room
      When a user outside the project joins the task's live room
      Then the join is refused as "forbidden"
