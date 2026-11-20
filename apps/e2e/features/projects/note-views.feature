@projects @interactions @views @docs
Feature: Note views
  A note view holds one editable document that belongs to the view: sprint
  goals, meeting minutes, a release checklist. It is added from the "Add view"
  popover like a Board, Table or Gantt view, in the Sprint, Backlog and
  Timeline interactions. The document opens in the same editor as the
  project's docs, with its comments and activity pane, but with no title and
  no list of notes. It needs the docs permissions, does not appear in the Docs
  sidebar, and is deleted with the view.

  @authenticated
  Rule: A note view is created from the "Add view" popover

    Background:
      Given the user is signed in as an administrator
      And a project exists

    Scenario: Creating a note view opens an editable document
      When the user opens the product backlog
      And the user adds a view named "Sprint notes" with the "Note" layout
      Then the "Sprint notes" view tab should be active
      And the view should show an editor, with no notes list
      When the user types "Ship note views" into the editor and saves
      Then the view's one document should be stored with that content

  @authenticated
  Rule: The document belongs to its view

    Background:
      Given the user is signed in as an administrator
      And a project exists with a backlog note view holding the document "Retro"

    Scenario: The document is not in the Docs sidebar
      When the user opens the note view
      Then the editor should be shown
      And the Docs sidebar should not list "Retro"
      And the view should still hold one document
