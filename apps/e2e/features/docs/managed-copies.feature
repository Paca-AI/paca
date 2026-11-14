@docs
Feature: Managed document copies
  A managed copy is a document that a coding agent pushes from a file in a
  code repository. Paca shows where it comes from. A locked copy is edited in
  its repository, not in Paca: it is read-only here and cannot be renamed,
  but it can still be moved or deleted. A member with docs:write can lock or
  unlock it.

  @authenticated
  Rule: Managed copies

    Background:
      Given the user already has a stored authenticated session
      And a project exists
      And the user is a member of the project with "docs:write" permission

    Scenario: A managed copy shows where it comes from
      Given an unlocked copy of "docs/plan.md" in "github.com/acme/app" was pushed
      When the user opens the document
      Then a banner says "Copy of docs/plan.md in github.com/acme/app at 0123456"
      And the banner says edits made here are overwritten by the next push
      And the document tree marks the document as a managed copy
      And a document created in Paca shows no banner and no marker

    Scenario: A locked managed copy is read-only and can be unlocked
      Given a locked copy of "docs/plan.md" was pushed
      When the user opens the document
      Then the banner says the copy is locked
      And the editor is read-only
      When the user clicks "Unlock"
      Then the editor accepts edits without a page reload
      And a saved edit reaches the server

    Scenario: Locking a document that is open stops further edits
      Given an unlocked managed copy is open in two browsers
      When one of them clicks "Lock"
      Then the editor is read-only in both browsers
      When the copy is pushed again while it is locked
      Then both browsers show the pushed content
      When one of them clicks "Unlock"
      Then both browsers show the stored content and edit it together again

    Scenario: A save that races a lock is dropped and not retried
      Given an unlocked managed copy is open with an unsaved edit
      When the document is locked before the save reaches the server
      Then the user is told the last edit was not saved
      And the editor is read-only and shows the stored content
      And the save is not sent again

    Scenario: Locking with an unsaved edit takes one click and keeps the edit
      Given an unlocked managed copy is open with an unsaved edit
      When the user clicks "Lock"
      Then the edit is saved before the lock is sent
      And the banner says the copy is locked
      And the stored document contains the edit

    Scenario: A locked managed copy offers no Revert in its activity
      Given a locked managed copy whose content was changed by a push
      When the user opens an "updated content" entry's actions
      Then "View diff" is offered and "Revert" is not
      When the user clicks "Unlock"
      Then "Revert" is offered again

    Scenario: A locked document cannot be renamed from the tree but can be moved
      Given a locked managed copy inside the folder "Guides"
      When the user opens the document options in the tree
      Then "Rename" is disabled and explains that the copy is locked
      When the user selects "Move to top level"
      Then the document is moved and stays locked
