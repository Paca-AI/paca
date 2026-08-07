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
  full event timeline and reply composer (ConversationView) via nested
  routes on $conversationId.

  @authenticated
  Rule: Project-scoped conversations list

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_CONV_PROJECT" exists
      And the user has navigated to the "E2E_CONV_PROJECT" project

    Scenario: A project with no conversations shows an empty state
      Given "E2E_CONV_PROJECT" has no conversations
      When the user navigates to the Conversations page for "E2E_CONV_PROJECT"
      Then the conversations list should display an empty state

    Scenario: The list shows a loading skeleton before conversations arrive
      When the user navigates to the Conversations page for "E2E_CONV_PROJECT"
      Then the conversations list should briefly display loading skeletons

    Scenario: Landing on the bare Conversations route shows the blank composer
      When the user navigates to the Conversations page for "E2E_CONV_PROJECT"
      Then the right-hand pane should display a blank composer
      And the composer should show an inline agent picker

    Scenario: A conversation list item shows the agent name, status badge, iteration count, and trigger label
      Given "E2E_CONV_PROJECT" has an agent named "E2E_CONV_AGENT"
      And "E2E_CONV_PROJECT" has a chat conversation with "E2E_CONV_AGENT" that has run 2 iterations
      When the user navigates to the Conversations page for "E2E_CONV_PROJECT"
      Then the conversations list should show an item for "E2E_CONV_AGENT"
      And that item should show "2 iterations"
      And that item should show the "Chat" trigger label

    Scenario: A task-assignment-triggered conversation is labelled distinctly from a chat conversation
      Given "E2E_CONV_PROJECT" has an agent named "E2E_CONV_TASK_AGENT"
      And "E2E_CONV_PROJECT" has a conversation with "E2E_CONV_TASK_AGENT" triggered by a task assignment
      When the user navigates to the Conversations page for "E2E_CONV_PROJECT"
      Then the conversations list should show the "Task" trigger label for that conversation

    Scenario: An automation-triggered conversation is labelled as automation-triggered
      Given "E2E_CONV_PROJECT" has an agent named "E2E_CONV_AUTO_AGENT"
      And "E2E_CONV_PROJECT" has a conversation with "E2E_CONV_AUTO_AGENT" triggered by an automation with no human actor
      When the user navigates to the Conversations page for "E2E_CONV_PROJECT"
      Then the conversations list should show the "Automation" trigger label for that conversation

    Scenario: Filtering the list by agent narrows the visible conversations
      Given "E2E_CONV_PROJECT" has an agent named "E2E_CONV_FILTER_AGENT_A"
      And "E2E_CONV_PROJECT" has an agent named "E2E_CONV_FILTER_AGENT_B"
      And "E2E_CONV_PROJECT" has a chat conversation with "E2E_CONV_FILTER_AGENT_A"
      And "E2E_CONV_PROJECT" has a chat conversation with "E2E_CONV_FILTER_AGENT_B"
      When the user navigates to the Conversations page for "E2E_CONV_PROJECT"
      And the user filters the conversations list by agent "E2E_CONV_FILTER_AGENT_A"
      Then the conversations list should show a conversation with "E2E_CONV_FILTER_AGENT_A"
      And the conversations list should not show a conversation with "E2E_CONV_FILTER_AGENT_B"

    Scenario: A filtered list with no matches shows the filtered empty state, distinct from the unfiltered empty state
      Given "E2E_CONV_PROJECT" has an agent named "E2E_CONV_NOMATCH_AGENT"
      When the user navigates to the Conversations page for "E2E_CONV_PROJECT"
      And the user filters the conversations list by agent "E2E_CONV_NOMATCH_AGENT"
      Then the conversations list should display the "no conversations match your filters" empty state

    Scenario: Scrolling to the bottom of a long conversation list loads the next page
      Given "E2E_CONV_PROJECT" has more than one page of conversations
      When the user navigates to the Conversations page for "E2E_CONV_PROJECT"
      And the user scrolls the conversations list to the bottom
      Then additional older conversations should be appended to the list

  @authenticated
  Rule: Starting a new project-scoped conversation

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_CONV_START_PROJECT" exists
      And "E2E_CONV_START_PROJECT" has an agent named "E2E_CONV_START_AGENT"
      And the user has navigated to the Conversations page for "E2E_CONV_START_PROJECT"

    Scenario: The composer's send control is disabled until an agent is selected
      Then the message composer's send action should be disabled

    Scenario: Selecting an agent in the inline picker enables the composer
      When the user selects "E2E_CONV_START_AGENT" in the inline agent picker
      Then the message composer's send action should be enabled

    Scenario: Sending the first message starts a chat session and navigates to the new conversation
      When the user selects "E2E_CONV_START_AGENT" in the inline agent picker
      And the user types "Please review the open pull requests" into the composer
      And the user sends the message
      Then the page URL should be the project conversation detail URL for the new conversation
      And the conversation view should display the message "Please review the open pull requests"

    Scenario: Clicking "New conversation" from an open conversation returns to the blank composer
      Given "E2E_CONV_START_PROJECT" has a chat conversation with "E2E_CONV_START_AGENT"
      When the user opens that conversation
      And the user clicks the "New conversation" button
      Then the right-hand pane should display a blank composer again

  @authenticated
  Rule: Viewing a project conversation's event timeline and controls

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_CONV_VIEW_PROJECT" exists
      And "E2E_CONV_VIEW_PROJECT" has an agent named "E2E_CONV_VIEW_AGENT"
      And the user has navigated to the Conversations page for "E2E_CONV_VIEW_PROJECT"

    Scenario: Opening a conversation shows its status badge and message timeline
      Given "E2E_CONV_VIEW_PROJECT" has a chat conversation with "E2E_CONV_VIEW_AGENT" that is "running"
      When the user opens that conversation
      Then the conversation header should display the "Running" status badge
      And the conversation timeline should render the recorded events as chat messages

    Scenario: A running chat conversation shows a Stop control
      Given "E2E_CONV_VIEW_PROJECT" has a chat conversation with "E2E_CONV_VIEW_AGENT" that is "running"
      When the user opens that conversation
      Then a "Stop" button should be visible in the conversation header

    Scenario: A finished conversation hides the Stop control
      Given "E2E_CONV_VIEW_PROJECT" has a chat conversation with "E2E_CONV_VIEW_AGENT" that is "finished"
      When the user opens that conversation
      Then no "Stop" button should be visible in the conversation header

    Scenario: A failed conversation with no messages shows a dedicated failure state
      Given "E2E_CONV_VIEW_PROJECT" has a chat conversation with "E2E_CONV_VIEW_AGENT" that is "failed" with no recorded events
      When the user opens that conversation
      Then a failure message should be displayed with the conversation's error message

    Scenario: A failed conversation that already produced messages still renders its timeline
      Given "E2E_CONV_VIEW_PROJECT" has a chat conversation with "E2E_CONV_VIEW_AGENT" that is "failed" with at least one recorded event
      When the user opens that conversation
      Then the conversation timeline should render the recorded events
      And a failure notice should still be shown in the footer

    Scenario: A branch name and PR link are shown when the conversation produced one
      Given "E2E_CONV_VIEW_PROJECT" has a chat conversation with "E2E_CONV_VIEW_AGENT" that produced a branch and a pull request
      When the user opens that conversation
      Then the conversation header should display the branch name
      And the conversation header should display a link to the pull request

    Scenario: Replying to a finished chat conversation silently starts a fresh conversation
      Given "E2E_CONV_VIEW_PROJECT" has a chat conversation with "E2E_CONV_VIEW_AGENT" that is "finished"
      When the user opens that conversation
      And the user types "One more thing" into the composer
      And the user sends the message
      Then the page URL should update to a different conversation identifier
      And the conversation timeline should include "One more thing"

    Scenario: A task-triggered conversation with no chat session has no reply composer
      Given "E2E_CONV_VIEW_PROJECT" has a conversation with "E2E_CONV_VIEW_AGENT" triggered by a task assignment with no chat session
      When the user opens that conversation
      Then the reply composer should be disabled

    Scenario: Navigating directly to a deleted conversation's URL shows an error state instead of crashing
      When the user navigates directly to a project conversation URL for a conversation that no longer exists
      Then a route error state should be displayed instead of a blank page

  @authenticated
  Rule: Global conversations page

    Background:
      Given the user already has a stored authenticated session

    Scenario: The global Conversations page lists the caller's own conversations with global agents
      Given the caller has a global chat conversation with a global agent named "E2E_CONV_GLOBAL_AGENT"
      When the user navigates to the global Conversations page
      Then the conversations list should show an item for "E2E_CONV_GLOBAL_AGENT"

    Scenario: The global Conversations page has no project filter scoping
      Given the caller has no global chat conversations
      When the user navigates to the global Conversations page
      Then the conversations list should display an empty state

    Scenario: Starting a new global conversation uses the chattable global agents list
      Given a global agent named "E2E_CONV_GLOBAL_START_AGENT" exists
      When the user navigates to the global Conversations page
      And the user selects "E2E_CONV_GLOBAL_START_AGENT" in the inline agent picker
      And the user types "What's on my plate today?" into the composer
      And the user sends the message
      Then the page URL should be the global conversation detail URL for the new conversation

    Scenario: Opening a global conversation renders the same timeline view as a project conversation
      Given the caller has a global chat conversation with a global agent named "E2E_CONV_GLOBAL_VIEW_AGENT"
      When the user navigates to the global Conversations page
      And the user opens that conversation
      Then the conversation header should display a status badge
      And the conversation timeline should render the recorded events as chat messages

    Scenario: Navigating directly to a deleted global conversation's URL shows an error state instead of crashing
      When the user navigates directly to a global conversation URL for a conversation that no longer exists
      Then a route error state should be displayed instead of a blank page
