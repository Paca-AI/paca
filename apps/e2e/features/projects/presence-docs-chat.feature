@projects @presence @docs @chat
Feature: Who is viewing and typing in a doc or a chat channel
  The presence layer that shows viewers and "<name> is typing…" on a task
  (task-presence.feature) works for the other places people write together:
  document comments and chat channels (project channels and direct messages).
  Access is checked once at join with the action that reads the thing
  (docs:read, channels:read), and typing is only accepted from people who can
  write there (docs:write, messages:write). The server also sends usernames, so
  a person who cannot list the project's members still sees who is there.

  Rule: Documents

    Background:
      Given a project with a doc, the admin, and a member who can edit docs

    Scenario: A second person opening the doc appears in the header
      When both open the doc
      Then each sees the other in the doc header's viewers

    Scenario: Typing a doc comment shows an indicator to the other person
      Given both have the doc's comments panel open
      When the member types in the comment composer
      Then the admin sees "<member> is typing…" above the composer
      And the indicator disappears when the member clears the composer

    Scenario: A member without the members permission is still named
      Given the member cannot list project members
      Then the admin's typing indicator and viewer title show the member's username

  Rule: Chat channels

    Background:
      Given a project with a public channel, the admin, and a member who can post

    Scenario: A second person opening the channel appears in the header
      When both open the channel
      Then each sees the other in the channel header's viewers

    Scenario: Typing a message shows an indicator, and sending clears it
      When the member types in the message box
      Then the admin sees "<member> is typing…" above the message box
      When the member sends the message
      Then the indicator disappears

    Scenario: A read-only member is a viewer but is never announced as typing
      Given a member who can read the channel but not post
      Then the admin sees them as a viewer
      And their typing messages over the socket are ignored

  Rule: Direct messages

    Scenario: A direct message has no project and still shows who is typing
      Given the admin and a member have a direct message open
      When the member types in the message box
      Then the admin sees "<member> is typing…"

    Scenario: A user outside a direct message cannot join its live room
      When a third user joins the direct message's live room
      Then the join is refused as "forbidden"
