@docs
Feature: Documentation
  The documentation feature lets project members create, organise, and
  collaboratively edit rich-text documents (using BlockNote) inside a
  project. Documents can be grouped in folders, each edit creates a
  snapshot for history tracking, and members can add threaded comments.

  @authenticated
  Rule: Document folders

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_DOCS_FOLDERS" exists
      And the user is a member of the project with "docs.write" permission
      And the user has navigated to the Docs page of "E2E_DOCS_FOLDERS"

    Scenario: Create a new folder
      When the user opens the "Add" menu in the Documentation section
      And the user selects "New Folder"
      And the user renames the newly created folder to "Architecture"
      Then a folder named "Architecture" should appear in the folder list

    Scenario: Rename an existing folder
      Given a folder named "Old Name" exists in the project
      When the user opens the folder options for "Old Name"
      And the user selects "Rename"
      And the user types "New Name" as the folder name
      And the user confirms the rename with Enter
      Then the folder list should show "New Name" instead of "Old Name"

    Scenario: Delete an existing folder
      Given a folder named "To Delete" exists in the project
      When the user opens the folder options for "To Delete"
      And the user selects "Delete"
      And the user confirms the deletion
      Then the folder named "To Delete" should no longer appear in the folder list

    Scenario: Member without write permission cannot create a folder
      Given the user is a member of the project with only "docs.read" permission
      Then the "Add" button in the Documentation section should not be visible

  @authenticated
  Rule: Document lifecycle

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_DOCS_LIFECYCLE" exists
      And the user is a member of the project with "docs.write" permission
      And the user has navigated to the Docs page of "E2E_DOCS_LIFECYCLE"

    Scenario: Create a document at the project root
      When the user opens the "Add" menu in the Documentation section
      And the user selects "New Document"
      Then a new document editor should open with title "Untitled"
      And the document should appear in the Documentation sidebar

    Scenario: Create a document inside a folder
      Given a folder named "Engineering" exists in the project
      When the user opens the "Add" menu and selects "New Document"
      Then the new document should be visible under the "Engineering" folder

    Scenario: Empty title defaults to "Untitled"
      When a document is created without a title
      Then the document title should be "Untitled"

    Scenario: Rename a document
      Given a document named "Draft" exists in the project
      When the user opens the document options for "Draft"
      And the user selects "Rename"
      And the user types "Final" as the document title
      And the user confirms the rename with Enter
      Then the document list should show "Final" instead of "Draft"

    Scenario: Delete a document
      Given a document named "Temporary Doc" exists in the project
      When the user opens the document options for "Temporary Doc"
      And the user selects "Delete"
      And the user confirms the deletion
      Then "Temporary Doc" should no longer appear in the document list

    Scenario: Member without write permission can view but not edit a document
      Given a document named "Read-Only Doc" exists in the project
      And the user is a member of the project with only "docs.read" permission
      When the user opens the document "Read-Only Doc"
      Then the document editor should be in read-only mode

  @authenticated
  Rule: Document editor with BlockNote

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_DOCS_EDITOR" exists
      And the user is a member of the project with "docs.write" permission
      And the user has navigated to the Docs page of "E2E_DOCS_EDITOR"
      And a document named "E2E_EDITOR_DOC" exists in the project

    Scenario: Editor loads existing document content
      When the user opens the document "E2E_EDITOR_DOC"
      Then the BlockNote editor should be visible
      And the document title should be displayed in the page header

    Scenario: User can type content into the editor
      When the user opens the document "E2E_EDITOR_DOC"
      And the user types "Hello World" into the editor
      And the user saves the document
      Then the editor should display "Hello World"

    Scenario: Saving creates a new snapshot
      Given the document "E2E_EDITOR_DOC" already has content "Version 1"
      When the user opens the document "E2E_EDITOR_DOC"
      And the user changes the content to "Version 2"
      And the user saves the document
      Then the document should have at least 1 snapshot

  @authenticated
  Rule: Document history and changes

    The standalone "Version history" panel was replaced by the "Comments &
    activity" feed: each content or title edit is logged as an "updated ..."
    entry with "View diff" and "Revert" actions. Saved edits still create
    snapshots that the API exposes.

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_DOCS_HISTORY" exists
      And the user is a member of the project with "docs.write" permission
      And a document named "E2E_HISTORY_DOC" whose content was updated from "Initial" to "Updated" exists in the project
      And the user has navigated to the document "E2E_HISTORY_DOC" in "E2E_DOCS_HISTORY"

    Scenario: User can view what changed in a document update
      When the user opens the "Comments & activity" panel
      And the user switches the feed to "All activity"
      And the user opens the options for the "updated content" entry
      And the user selects "View diff"
      Then a "Content change diff" dialog should show the "Initial" and "Updated" text

    Scenario: User can revert a document update
      When the user opens the "Comments & activity" panel
      And the user switches the feed to "All activity"
      And the user opens the options for the "updated content" entry
      And the user selects "Revert"
      Then the document content should be restored to "Initial"

  @authenticated
  Rule: Document comments and activity

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_DOCS_COMMENTS" exists
      And the user is a member of the project with "docs.write" permission
      And a document named "E2E_COMMENT_DOC" exists in the project
      And the user has navigated to the document "E2E_COMMENT_DOC" in "E2E_DOCS_COMMENTS"

    Scenario: User can add a comment to a document
      When the user opens the "Comments & activity" panel
      And the user types "Great document!" in the comment input
      And the user presses Ctrl+Enter to submit the comment
      Then the comment "Great document!" should appear in the activity panel

    Scenario: User can edit their own comment
      Given the user has posted a comment "Original comment"
      When the user opens the "Comments & activity" panel
      And the user opens the comment options for "Original comment"
      And the user selects "Edit"
      And the user replaces the comment text in the composer with "Updated comment"
      And the user presses Ctrl+Enter to save the comment
      Then the activity panel should show "Updated comment"

    Scenario: User can delete their own comment
      Given the user has posted a comment "Delete me"
      When the user opens the "Comments & activity" panel
      And the user opens the comment options for "Delete me"
      And the user selects "Delete"
      Then the comment "Delete me" should no longer appear in the activity panel

    Scenario: Activity log shows document creation event
      When the user opens the "Comments & activity" panel
      And the user switches the feed to "All activity"
      Then the activity panel should contain a "created this document" entry

    Scenario: Comment input accepts Ctrl+Enter keyboard shortcut
      When the user opens the "Comments & activity" panel
      And the user types "Keyboard shortcut test" in the comment input
      And the user presses Ctrl+Enter
      Then the comment should be submitted and the input should be cleared
