@projects @interactions @views @docs
Feature: Note views
  A note view holds free-form writing that belongs to one view: sprint goals,
  meeting minutes, a release checklist. It is added from the "Add view" popover
  like a Board, Table or Gantt view, in the Sprint, Backlog and Timeline
  interactions. Its notes are documents: they open in the same editor as the
  project's docs, keep their history and comments, and need the docs
  permissions. They belong to the view, not to the Docs sidebar, and they are
  deleted with the view.

  @authenticated
  Rule: A note view is created from the "Add view" popover

    Background:
      Given the user is signed in as an administrator
      And a project exists

    Scenario: Creating a note view and writing its first note
      When the user opens the product backlog
      And the user adds a view named "Sprint notes" with the "Note" layout
      Then the "Sprint notes" view tab should be active
      And the view should say that it has no notes yet
      When the user clicks "New note"
      And the user names the note "Goals"
      Then the note "Goals" should be listed and open in the editor
      When the user types "Ship note views" into the editor and saves
      Then the note's content should be stored

  @authenticated
  Rule: Notes belong to their view

    Background:
      Given the user is signed in as an administrator
      And a project exists with a backlog note view holding the notes "Retro" and "Demo"

    Scenario: Notes are listed in the view and not in the Docs sidebar
      When the user opens the note view
      Then the notes "Retro" and "Demo" should be listed in that order
      And the Docs sidebar should not list "Retro"

    Scenario: A note is moved down
      When the user opens the note view
      And the user moves "Retro" down
      Then the notes should be listed as "Demo" then "Retro"

    Scenario: Deleting a note asks for confirmation
      When the user opens the note view
      And the user chooses "Delete" for the note "Retro"
      Then a confirmation dialog should name "Retro"
      When the user confirms
      Then "Retro" should no longer be listed
      And the note should be deleted
