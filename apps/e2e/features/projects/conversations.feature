@projects @agents @conversations
Feature: Agent conversations
  A conversation is one exchange between a human and an AI agent, whether
  it was started as an ad-hoc chat, fired automatically when a task was
  assigned to the agent, or triggered by an automation. The Conversations
  page comes in two scopes that share one layout component
  (ConversationsLayout): a project-scoped one
  (routes/_authenticated/projects/$projectId/conversations.tsx, listing
  conversations with that project's own agents) and a global one
  (routes/_authenticated/conversations.tsx, listing the caller's own
  conversations with global agents from the home/admin area). Both render a
  filterable, infinitely-scrollable conversation list in a left rail next
  to an Outlet: landing on the bare "/conversations" route (or clicking
  "New conversation") renders a blank composer with an inline agent picker
  (NewConversationThread); clicking an existing conversation renders its
  header and event timeline (ConversationView) via nested routes on
  $conversationId.

  A list item shows the conversation's title (falling back to the agent's
  name until Goose names the session or the user renames it), a status
  badge, a trigger label ("Chat", "Task", "Automation" or "Write
  description"), token usage once any was recorded, and the creation date.
  It does NOT show an iteration count. The detail header shows "Chat
  session" or "Task session" (not the title), the status badge, and — when
  present — the branch name, a "PR" link, and a Stop button while the
  conversation is queued, running or paused.

  The e2e stack has no valid LLM credentials, so a seeded chat conversation
  always ends "Failed" within a few seconds ("Authentication required").
  Scenarios that need a specific status, trigger, or event history override
  the API response instead of relying on a live agent.

  @authenticated
  Rule: Project-scoped conversations list

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_CONV_LIST_PROJECT" exists
      And the user has navigated to the "E2E_CONV_LIST_PROJECT" project

    Scenario: A project with no conversations shows an empty state
      Given "E2E_CONV_LIST_PROJECT" has no conversations
      When the user navigates to the Conversations page for "E2E_CONV_LIST_PROJECT"
      Then the conversations list should display the "No conversations yet" empty state

    Scenario: The list shows loading skeletons before conversations arrive
      Given the conversations request for "E2E_CONV_LIST_PROJECT" is slow
      When the user navigates to the Conversations page for "E2E_CONV_LIST_PROJECT"
      Then the conversations list should briefly display loading skeletons

    Scenario: Landing on the bare Conversations route shows the blank composer
      Given "E2E_CONV_LIST_PROJECT" has two agents
      When the user navigates to the Conversations page for "E2E_CONV_LIST_PROJECT"
      Then the right-hand pane should display a blank composer
      And the composer should show an inline agent picker with the "Select an agent…" placeholder

    Scenario: A conversation list item shows the agent name, status badge, and trigger label
      Given "E2E_CONV_LIST_PROJECT" has an agent named "E2E_CONV_AGENT"
      And "E2E_CONV_LIST_PROJECT" has a chat conversation with "E2E_CONV_AGENT"
      When the user navigates to the Conversations page for "E2E_CONV_LIST_PROJECT"
      Then the conversations list should show an item for "E2E_CONV_AGENT"
      And that item should show the "Chat" trigger label
      And that item should show a status badge

    Scenario: A task-assignment-triggered conversation is labelled "Task"
      Given "E2E_CONV_LIST_PROJECT" has a conversation triggered by a task assignment by a member
      When the user navigates to the Conversations page for "E2E_CONV_LIST_PROJECT"
      Then the conversations list should show the "Task" trigger label for that conversation

    Scenario: A conversation triggered by an automation with no human actor is labelled "Automation"
      Given "E2E_CONV_LIST_PROJECT" has a conversation triggered by an automation with no human actor
      When the user navigates to the Conversations page for "E2E_CONV_LIST_PROJECT"
      Then the conversations list should show the "Automation" trigger label for that conversation

    Scenario: Filtering the list by agent narrows the visible conversations
      Given "E2E_CONV_LIST_PROJECT" has an agent named "E2E_CONV_FILTER_A"
      And "E2E_CONV_LIST_PROJECT" has an agent named "E2E_CONV_FILTER_B"
      And "E2E_CONV_LIST_PROJECT" has a chat conversation with "E2E_CONV_FILTER_A"
      And "E2E_CONV_LIST_PROJECT" has a chat conversation with "E2E_CONV_FILTER_B"
      When the user navigates to the Conversations page for "E2E_CONV_LIST_PROJECT"
      And the user filters the conversations list by agent "E2E_CONV_FILTER_A"
      Then the conversations list should show a conversation with "E2E_CONV_FILTER_A"
      And the conversations list should not show a conversation with "E2E_CONV_FILTER_B"

    Scenario: A filtered list with no matches shows the filtered empty state
      Given "E2E_CONV_LIST_PROJECT" has an agent named "E2E_CONV_NOMATCH_A"
      And "E2E_CONV_LIST_PROJECT" has an agent named "E2E_CONV_NOMATCH_B"
      And "E2E_CONV_LIST_PROJECT" has a chat conversation with "E2E_CONV_NOMATCH_B"
      When the user navigates to the Conversations page for "E2E_CONV_LIST_PROJECT"
      And the user filters the conversations list by agent "E2E_CONV_NOMATCH_A"
      Then the conversations list should display the "No matching conversations" empty state

    Scenario: Scrolling to the bottom of a long conversation list loads the next page
      Given "E2E_CONV_LIST_PROJECT" has more than one page of conversations
      When the user navigates to the Conversations page for "E2E_CONV_LIST_PROJECT"
      And the user scrolls the conversations list to the bottom
      Then additional older conversations should be appended to the list

  @authenticated
  Rule: Conversations permissions in a project

    A chat conversation is private to the member who started it, so the
    read-only scenarios have the list API return a project-shared
    conversation (as a task-triggered one would be) instead of relying on a
    conversation another member started.

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_CONV_PERM_PROJECT" exists

    Scenario: A member without conversations.read sees the no-permission state instead of the list
      Given a project member who does not have the "conversations.read" permission
      When that member navigates to the Conversations page for "E2E_CONV_PERM_PROJECT"
      Then the conversations list should display "You don't have permission to view conversations"

    Scenario: A member with only conversations.read can browse the list but not manage it
      Given a project member who has the "conversations.read" permission only
      And the conversations list contains a project-shared conversation titled "E2E_CONV_PERM_SHARED"
      When that member navigates to the Conversations page for "E2E_CONV_PERM_PROJECT"
      Then the conversations list should show an item titled "E2E_CONV_PERM_SHARED"
      And the "New conversation" button should not be visible
      And no "More actions" menu should be offered on the item

    Scenario: A member with only conversations.read cannot use the composer
      Given a project member who has the "conversations.read" permission only
      When that member navigates to the Conversations page for "E2E_CONV_PERM_PROJECT"
      Then no message composer should be offered

  @authenticated
  Rule: Starting a new project-scoped conversation

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_CONV_START_PROJECT" exists
      And "E2E_CONV_START_PROJECT" has two agents named "E2E_CONV_START_AGENT" and "E2E_CONV_OTHER_AGENT"
      And the user has navigated to the Conversations page for "E2E_CONV_START_PROJECT"

    Scenario: The send action is disabled until an agent is selected
      When the user types "Please review the open pull requests" into the composer
      Then the send action should be disabled

    Scenario: Selecting an agent in the inline picker enables the send action
      When the user types "Please review the open pull requests" into the composer
      And the user selects "E2E_CONV_START_AGENT" in the inline agent picker
      Then the send action should be enabled

    Scenario: A project with a single agent auto-selects it
      Given a project named "E2E_CONV_SOLO_PROJECT" with a single agent named "E2E_CONV_SOLO_AGENT"
      When the user navigates to the Conversations page for "E2E_CONV_SOLO_PROJECT"
      And the user types "Hello" into the composer
      Then the send action should be enabled

    Scenario: Sending the first message starts a chat session and opens the new conversation
      When the user selects "E2E_CONV_START_AGENT" in the inline agent picker
      And the user types "Please review the open pull requests" into the composer
      And the user sends the message
      Then the page URL should be the project conversation detail URL for the new conversation
      And the conversation timeline should display the message "Please review the open pull requests"

    Scenario: Clicking "New conversation" from an open conversation returns to the blank composer
      Given "E2E_CONV_START_PROJECT" has a chat conversation with "E2E_CONV_START_AGENT"
      When the user opens that conversation
      And the user clicks the "New conversation" button
      Then the right-hand pane should display a blank composer again

  @authenticated
  Rule: Viewing a project conversation's timeline and controls

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_CONV_VIEW_PROJECT" exists
      And "E2E_CONV_VIEW_PROJECT" has an agent named "E2E_CONV_VIEW_AGENT"

    Scenario: Opening a conversation shows the "Chat session" header, its status badge, and its messages
      Given "E2E_CONV_VIEW_PROJECT" has a chat conversation with "E2E_CONV_VIEW_AGENT" that has ended
      When the user opens that conversation
      Then the conversation header should display "Chat session" and the "Failed" status badge
      And the conversation timeline should display the message that started the conversation

    Scenario: A running conversation shows the status badge and a Stop control
      Given "E2E_CONV_VIEW_PROJECT" has a chat conversation with "E2E_CONV_VIEW_AGENT" that the API reports as running
      When the user opens that conversation
      Then the conversation header should display the "Running" status badge
      And a "Stop" button should be visible in the conversation header

    Scenario: An ended conversation hides the Stop control
      Given "E2E_CONV_VIEW_PROJECT" has a chat conversation with "E2E_CONV_VIEW_AGENT" that has ended
      When the user opens that conversation
      Then no "Stop" button should be visible in the conversation header

    Scenario: A failed conversation with no messages shows a dedicated failure state
      Given "E2E_CONV_VIEW_PROJECT" has a failed conversation with no recorded events and the error "Sandbox crashed"
      When the user opens that conversation
      Then a "Conversation failed" state should be displayed with the message "Sandbox crashed"

    Scenario: A failed conversation that already produced messages still renders its timeline
      Given "E2E_CONV_VIEW_PROJECT" has a chat conversation with "E2E_CONV_VIEW_AGENT" that failed with the error "Sandbox crashed"
      When the user opens that conversation
      Then the conversation timeline should display the message that started the conversation
      And the failure notice "Sandbox crashed" should be shown

    Scenario: A branch name and PR link are shown when the conversation produced one
      Given "E2E_CONV_VIEW_PROJECT" has a chat conversation with "E2E_CONV_VIEW_AGENT" that produced the branch "feature/e2e-conv" and a pull request
      When the user opens that conversation
      Then the conversation header should display the branch name "feature/e2e-conv"
      And the conversation header should display a "PR" link to the pull request

    Scenario: The composer of an ended chat conversation is not offered
      Given "E2E_CONV_VIEW_PROJECT" has a chat conversation with "E2E_CONV_VIEW_AGENT" that has ended
      When the user opens that conversation
      Then no message composer should be offered

    Scenario: A task-triggered conversation with no chat session has no reply composer
      Given "E2E_CONV_VIEW_PROJECT" has a conversation triggered by a task assignment with no chat session
      When the user opens that conversation
      Then no message composer should be offered

    Scenario: Navigating directly to a conversation that does not exist shows a not-found state
      When the user navigates directly to a project conversation URL for a conversation that does not exist
      Then a "Conversation not found" state should be displayed

    Scenario: A live agent reply streams into the timeline
      Given "E2E_CONV_VIEW_PROJECT" has a chat conversation with a working LLM-backed agent
      When the user opens that conversation
      Then the agent's reply should stream into the conversation timeline

  @authenticated
  Rule: Renaming and deleting a project conversation

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_CONV_MANAGE_PROJECT" exists
      And "E2E_CONV_MANAGE_PROJECT" has an agent named "E2E_CONV_MANAGE_AGENT"
      And "E2E_CONV_MANAGE_PROJECT" has a chat conversation with "E2E_CONV_MANAGE_AGENT"

    Scenario: Renaming a conversation replaces the agent name in the list with the new title
      Given the user has navigated to the Conversations page for "E2E_CONV_MANAGE_PROJECT"
      When the user opens the "More actions" menu on the conversation and selects "Rename"
      And the user enters the title "E2E_CONV_RENAMED" and confirms with "Rename"
      Then the conversations list should show an item titled "E2E_CONV_RENAMED"
      And the conversation should be saved with the title "E2E_CONV_RENAMED"

    Scenario: A renamed conversation keeps its title after reloading the page
      Given the conversation has been renamed to "E2E_CONV_PERSISTED"
      When the user navigates to the Conversations page for "E2E_CONV_MANAGE_PROJECT"
      Then the conversations list should show an item titled "E2E_CONV_PERSISTED"

    Scenario: The rename dialog opens pre-filled with the current title
      Given the conversation has been renamed to "E2E_CONV_PREFILLED"
      And the user has navigated to the Conversations page for "E2E_CONV_MANAGE_PROJECT"
      When the user opens the "More actions" menu on the conversation and selects "Rename"
      Then the rename dialog should contain "E2E_CONV_PREFILLED"

    Scenario: Cancelling the rename dialog leaves the conversation unchanged
      Given the user has navigated to the Conversations page for "E2E_CONV_MANAGE_PROJECT"
      When the user opens the "More actions" menu on the conversation and selects "Rename"
      And the user types "E2E_CONV_DISCARDED" into the rename dialog and clicks "Cancel"
      Then the conversations list should show an item for "E2E_CONV_MANAGE_AGENT"
      And the conversations list should not show an item titled "E2E_CONV_DISCARDED"

    Scenario: The rename action is disabled while the title is blank
      Given the user has navigated to the Conversations page for "E2E_CONV_MANAGE_PROJECT"
      When the user opens the "More actions" menu on the conversation and selects "Rename"
      And the user clears the rename field
      Then the "Rename" button in the dialog should be disabled

    Scenario: The rename field accepts at most 200 characters
      Given the user has navigated to the Conversations page for "E2E_CONV_MANAGE_PROJECT"
      When the user opens the "More actions" menu on the conversation and selects "Rename"
      And the user types 250 characters into the rename field
      Then the rename field should contain exactly 200 characters

    Scenario: A failed rename keeps the dialog open and shows an error
      Given renaming conversations is failing on the server
      And the user has navigated to the Conversations page for "E2E_CONV_MANAGE_PROJECT"
      When the user opens the "More actions" menu on the conversation and selects "Rename"
      And the user enters the title "E2E_CONV_WONT_SAVE" and confirms with "Rename"
      Then the rename dialog should stay open and show "Couldn't rename the conversation. Please try again."

    Scenario: Deleting a conversation asks for confirmation naming the conversation
      Given the user has navigated to the Conversations page for "E2E_CONV_MANAGE_PROJECT"
      When the user opens the "More actions" menu on the conversation and selects "Delete"
      Then a "Delete conversation?" dialog should appear naming "E2E_CONV_MANAGE_AGENT"

    Scenario: Cancelling the delete confirmation keeps the conversation
      Given the user has navigated to the Conversations page for "E2E_CONV_MANAGE_PROJECT"
      When the user opens the "More actions" menu on the conversation and selects "Delete"
      And the user cancels the delete confirmation
      Then the conversations list should show an item for "E2E_CONV_MANAGE_AGENT"

    Scenario: Confirming the delete removes the conversation from the list
      Given the user has navigated to the Conversations page for "E2E_CONV_MANAGE_PROJECT"
      When the user opens the "More actions" menu on the conversation and selects "Delete"
      And the user confirms the deletion
      Then the conversations list should display the "No conversations yet" empty state
      And the conversation should no longer be retrievable

    Scenario: Deleting the open conversation returns to the blank composer
      Given the user has opened that conversation
      When the user opens the "More actions" menu on the conversation and selects "Delete"
      And the user confirms the deletion
      Then the page URL should be the bare Conversations URL of "E2E_CONV_MANAGE_PROJECT"
      And the right-hand pane should display a blank composer

    Scenario: A failed delete keeps the dialog open and shows an error
      Given deleting conversations is failing on the server
      And the user has navigated to the Conversations page for "E2E_CONV_MANAGE_PROJECT"
      When the user opens the "More actions" menu on the conversation and selects "Delete"
      And the user confirms the deletion
      Then the delete dialog should stay open and show "Couldn't delete the conversation. Please try again."

    Scenario: Navigating directly to a deleted conversation's URL shows a not-found state
      Given the conversation has been deleted
      When the user navigates directly to that conversation's URL
      Then a "Conversation not found" state should be displayed

  Rule: Conversation title and delete API contract

    Background:
      Given a project named "E2E_CONV_API_PROJECT" exists
      And "E2E_CONV_API_PROJECT" has a chat conversation with an agent

    Scenario: A blank title is rejected
      When the title of the conversation is set to whitespace only
      Then the API should respond with 400 and "title is required"

    Scenario: A title over 200 characters is rejected
      When the title of the conversation is set to 201 characters
      Then the API should respond with 400 and "title exceeds 200 characters"

    Scenario: The 200-character limit counts characters, not bytes
      When the title of the conversation is set to 200 Cyrillic characters
      Then the API should accept the title

    Scenario: A deleted conversation is soft-deleted from the user's point of view
      When the conversation is deleted
      Then fetching it should respond with 404
      And it should no longer appear in the project's conversation list
      And renaming it should respond with 404

  @authenticated
  Rule: Global conversations page

    Background:
      Given the user already has a stored authenticated session
      And a global agent named "E2E_CONV_GLOBAL_AGENT" exists

    Scenario: The global Conversations page lists the caller's own conversations with global agents
      Given the caller has a global chat conversation with "E2E_CONV_GLOBAL_AGENT"
      When the user navigates to the global Conversations page
      Then the conversations list should show an item for "E2E_CONV_GLOBAL_AGENT"

    Scenario: The global Conversations page shows an empty state when the caller has no conversations
      Given the caller has no global conversations
      When the user navigates to the global Conversations page
      Then the conversations list should display the "No conversations yet" empty state

    Scenario: Starting a new global conversation uses the chattable global agents list
      Given a second global agent named "E2E_CONV_GLOBAL_OTHER" exists
      When the user navigates to the global Conversations page
      And the user selects "E2E_CONV_GLOBAL_AGENT" in the inline agent picker
      And the user types "What's on my plate today?" into the composer
      And the user sends the message
      Then the page URL should be the global conversation detail URL for the new conversation
      And the conversation timeline should display the message "What's on my plate today?"

    Scenario: Opening a global conversation renders the same header and timeline as a project conversation
      Given the caller has a global chat conversation with "E2E_CONV_GLOBAL_AGENT" that has ended
      When the user navigates to the global Conversations page
      And the user opens that conversation
      Then the conversation header should display "Chat session" and the "Failed" status badge
      And the conversation timeline should display the message that started the conversation

    Scenario: A global conversation can be renamed and deleted by its owner
      Given the caller has a global chat conversation with "E2E_CONV_GLOBAL_AGENT"
      When the user navigates to the global Conversations page
      And the user renames the conversation to "E2E_CONV_GLOBAL_RENAMED"
      Then the conversations list should show an item titled "E2E_CONV_GLOBAL_RENAMED"
      When the user deletes the conversation
      Then the conversations list should display the "No conversations yet" empty state

    Scenario: Navigating directly to a global conversation that does not exist shows a not-found state
      When the user navigates directly to a global conversation URL for a conversation that does not exist
      Then a "Conversation not found" state should be displayed
