@profile @api-keys
Feature: API keys
  The API Keys page (/profile/api-keys) lets a user create personal API keys
  for authenticating API requests without a session cookie. A key is shown
  in full exactly once — in the "API key created" dialog right after
  creation — and afterwards only as "paca_" plus its first 8 characters.
  Keys can carry an optional expiration date and are revoked (deleted)
  after a confirmation. Keys are private to their owner and are managed only
  through a cookie/JWT session (GET/POST/DELETE /users/me/api-keys).

  @authenticated
  Rule: Empty state and layout

    Background:
      Given a user "E2E_APIKEYS" exists with no API keys
      And the user is signed in as "E2E_APIKEYS"
      And the user has navigated to the API Keys page

    Scenario: A user with no keys sees the empty state
      Then the page should display the "API Keys" heading
      And the page should display the "Your keys" card
      And the page should display "No API keys yet. Create one to get started."
      And a "New key" button should be visible

  @authenticated
  Rule: Creating an API key

    Background:
      Given a user "E2E_APIKEYS" exists with no API keys
      And the user is signed in as "E2E_APIKEYS"
      And the user has navigated to the API Keys page

    Scenario: The Create key button requires a name
      When the user clicks "New key"
      Then the "Create API key" dialog should open
      And the "Create key" button should be disabled
      When the user fills the "Name" field with "CI pipeline"
      Then the "Create key" button should be enabled

    Scenario: Creating a key reveals the full key exactly once
      When the user clicks "New key"
      And the user fills the "Name" field with "CI pipeline"
      And the user clicks "Create key"
      Then the "API key created" dialog should open
      And the dialog should display the key name "CI pipeline"
      And the dialog should display the full key starting with "paca_"
      And a "Copy key" button should be visible
      When the user clicks "Done"
      Then the "API key created" dialog should close
      And the key list should show "CI pipeline" with only the "paca_" prefix and first 8 characters followed by an ellipsis
      And the full key should not appear anywhere on the page
      And the "Expires" and "Last used" cells for "CI pipeline" should show "—"

    Scenario: The copy button confirms the key was copied
      When the user creates a key named "Copy test"
      And the user clicks "Copy key"
      Then the dialog should display "Copied to clipboard!"

    Scenario: An optional expiration date is shown in the key list
      When the user clicks "New key"
      And the user fills the "Name" field with "Short lived"
      And the user sets the "Expiration date" field to a date in 2099
      And the user clicks "Create key"
      And the user clicks "Done"
      Then the "Expires" cell for "Short lived" should show a date in 2099

    Scenario: A name longer than 100 characters is rejected
      When the user clicks "New key"
      And the user fills the "Name" field with 101 characters
      And the user clicks "Create key"
      Then the dialog should display "Name must be 100 characters or fewer."
      And the "Create API key" dialog should remain open

    Scenario: Cancelling the create dialog discards the draft
      When the user clicks "New key"
      And the user fills the "Name" field with "Never created"
      And the user clicks "Cancel"
      Then the "Create API key" dialog should close
      And the page should still display the empty state
      When the user clicks "New key"
      Then the "Name" field should be empty

  @authenticated
  Rule: Revoking an API key

    Background:
      Given a user "E2E_APIKEYS" exists
      And the user has API keys named "Keep me" and "Revoke me"
      And the user is signed in as "E2E_APIKEYS"
      And the user has navigated to the API Keys page

    Scenario: Revoking asks for confirmation and then removes the key
      When the user clicks the "Revoke key" button on the "Revoke me" row
      Then the "Revoke API key" dialog should open
      And the dialog should name "Revoke me"
      And the dialog should warn that requests using the key stop working immediately
      When the user confirms with "Revoke key"
      Then "Revoke me" should no longer appear in the key list
      And "Keep me" should still appear in the key list
      And the API key list should no longer include "Revoke me"

    Scenario: Cancelling the revoke confirmation keeps the key
      When the user clicks the "Revoke key" button on the "Revoke me" row
      And the user clicks "Cancel"
      Then the "Revoke API key" dialog should close
      And "Revoke me" should still appear in the key list

  @authenticated
  Rule: API key access control

    Scenario: Listing keys never exposes the raw key or its hash
      Given a user "E2E_APIKEYS" has an API key named "Listed"
      When the user requests GET /users/me/api-keys
      Then the listed key should include "key_prefix"
      And the listed key should not include "key" or "key_hash"

    Scenario: Keys are private to their owner
      Given a user "E2E_APIKEYS" has an API key named "Private"
      And another user "E2E_APIKEYS_OTHER" exists
      When the other user requests GET /users/me/api-keys
      Then the other user's list should not include "Private"
      When the other user sends DELETE /users/me/api-keys/{id} for the first user's key
      Then the request should be rejected

    Scenario: An unauthenticated client cannot list keys
      When an unauthenticated client requests GET /users/me/api-keys
      Then the response should be 401 Unauthorized

    Scenario: Creating a key with a blank name is rejected
      Given a user "E2E_APIKEYS" exists
      When the user sends POST /users/me/api-keys with a blank name
      Then the request should be rejected with error code "API_KEY_NAME_INVALID"
