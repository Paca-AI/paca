@projects @environments
Feature: Project environments
  Environments are named, long-lived sandboxes a project's agents can attach
  to across conversations. The Environments page
  (routes/_authenticated/projects/$projectId/environments/) lists each one as
  a card with its name, slug, backend badge ("docker" or "kubernetes"), and a
  status label. Creating an environment queues real provisioning: the API
  returns the new environment immediately in the "Creating" status and a
  background worker then asks agent-runner to launch its container, so the
  status keeps changing (Creating, Starting, Running, Error, ...) and the
  final status depends on whether the stack can launch sandboxes at all.
  These scenarios therefore assert that a status is shown, never which one.
  A restricted environment shows a "Restricted" badge to every member who
  holds no explicit access grant. Viewing the page is gated by
  environments.read; the "New Environment" button and every write action on
  the detail page are gated by environments.write. Live terminal, SSH connect
  and port-forward tunnelling need a running sandbox container and are out of
  scope here.

  @authenticated
  Rule: Environments page — empty state and permission gating

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_ENV_PROJECT" exists

    Scenario: A project with no environments shows an empty state
      Given "E2E_ENV_PROJECT" has no environments
      When the user navigates to the Environments page for "E2E_ENV_PROJECT"
      Then the Environments page should display the "No environments yet" empty state
      And the empty state should offer a "Create your first environment" action

    Scenario: The "New Environment" button is visible with environments.write permission
      Given the user has the "environments.read" and "environments.write" project permissions in "E2E_ENV_PROJECT"
      When the user navigates to the Environments page for "E2E_ENV_PROJECT"
      Then the "New Environment" button should be visible

    Scenario: The "New Environment" button is hidden without environments.write permission
      Given the user has only the "environments.read" project permission in "E2E_ENV_PROJECT"
      When the user navigates to the Environments page for "E2E_ENV_PROJECT"
      Then the "New Environment" button should not be visible
      And the empty state should not offer a "Create your first environment" action

    Scenario: A member without environments.read permission sees the no-permission state
      Given the user does not have the "environments.read" project permission in "E2E_ENV_PROJECT"
      When the user navigates to the Environments page for "E2E_ENV_PROJECT"
      Then the Environments page should display the "You don't have permission to view environments" message
      And the "New Environment" button should not be visible

    Scenario: Opening the page with create=true opens the create dialog
      When the user navigates to the Environments page for "E2E_ENV_PROJECT" with "?create=true"
      Then the "Create Environment" dialog should open
      When the user clicks "Cancel"
      Then the "Create Environment" dialog should close
      And the page URL should no longer request the create dialog

  @authenticated
  Rule: Creating an environment

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_ENV_CREATE_PROJECT" exists
      And the user has navigated to the Environments page for "E2E_ENV_CREATE_PROJECT"

    Scenario: The create dialog offers a name, a Docker access switch, and an Advanced section
      When the user clicks the "New Environment" button
      Then the "Create Environment" dialog should open
      And it should show a required "Name" field, a "Docker access" switch, and an "Advanced" disclosure

    Scenario: The "Create Environment" button is disabled until a name is entered
      When the user clicks the "New Environment" button
      Then the dialog's "Create Environment" button should be disabled
      When the user fills the environment name with "E2E_ENV_NEW"
      Then the dialog's "Create Environment" button should be enabled

    Scenario: The Advanced section reveals the image and resource limit fields
      When the user clicks the "New Environment" button
      And the user expands the "Advanced" section
      Then the dialog should show "Image", "CPU", "Memory" and "Disk" fields
      And it should hint that leaving them blank uses the platform defaults

    Scenario: A CPU limit below the minimum is rejected before submitting
      When the user clicks the "New Environment" button
      And the user fills the environment name with "E2E_ENV_BAD_CPU"
      And the user expands the "Advanced" section
      And the user fills the CPU limit with "0.01"
      Then the dialog should display "CPU must be at least 0.1 (100m)."
      And the dialog's "Create Environment" button should be disabled

    Scenario: A memory limit below the minimum is rejected before submitting
      When the user clicks the "New Environment" button
      And the user fills the environment name with "E2E_ENV_BAD_MEMORY"
      And the user expands the "Advanced" section
      And the user fills the memory limit with "100Mi"
      Then the dialog should display "Memory must be at least 256Mi."
      And the dialog's "Create Environment" button should be disabled

    Scenario: A disk limit that is not a positive whole number is rejected before submitting
      When the user clicks the "New Environment" button
      And the user fills the environment name with "E2E_ENV_BAD_DISK"
      And the user expands the "Advanced" section
      And the user fills the disk limit with "0"
      Then the dialog should display "Disk must be a positive whole number of GB."
      And the dialog's "Create Environment" button should be disabled

    Scenario: Cancelling the create dialog discards the in-progress environment
      When the user clicks the "New Environment" button
      And the user fills the environment name with "E2E_ENV_CANCELLED"
      And the user clicks "Cancel"
      Then the "Create Environment" dialog should close
      And the Environments page should not list "E2E_ENV_CANCELLED"

    Scenario: A newly created environment appears right away with a status and backend badge
      When the user clicks the "New Environment" button
      And the user fills the environment name with "E2E_ENV_CREATED"
      And the user clicks the dialog's "Create Environment" button
      Then the "Create Environment" dialog should close
      And the user should remain on the Environments page
      And the grid should show a card for "E2E_ENV_CREATED" with its slug
      And the card should display a status label
      And the card should display a backend badge

  @authenticated
  Rule: Environment cards

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_ENV_CARDS_PROJECT" exists

    Scenario: An environment card shows its name, slug, backend badge, and status
      Given "E2E_ENV_CARDS_PROJECT" has an environment named "E2E_ENV_CARD"
      When the user navigates to the Environments page for "E2E_ENV_CARDS_PROJECT"
      Then the card for "E2E_ENV_CARD" should display its name and slug
      And the card should display a backend badge
      And the card should display a status label

    Scenario: Clicking an environment card opens its detail page
      Given "E2E_ENV_CARDS_PROJECT" has an environment named "E2E_ENV_OPEN_CARD"
      When the user navigates to the Environments page for "E2E_ENV_CARDS_PROJECT"
      And the user clicks the card for "E2E_ENV_OPEN_CARD"
      Then the page URL should be the detail URL of "E2E_ENV_OPEN_CARD"

    Scenario: A restricted environment shows a "Restricted" badge to a member with no access grant
      Given "E2E_ENV_CARDS_PROJECT" has a restricted environment named "E2E_ENV_LOCKED"
      And "E2E_ENV_CARDS_PROJECT" has an open environment named "E2E_ENV_UNLOCKED"
      And the user has only the "environments.read" project permission in "E2E_ENV_CARDS_PROJECT"
      When the user navigates to the Environments page for "E2E_ENV_CARDS_PROJECT"
      Then the card for "E2E_ENV_LOCKED" should display a "Restricted" badge
      And the card for "E2E_ENV_UNLOCKED" should not display a "Restricted" badge

  @authenticated
  Rule: Environment detail — Overview tab

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_ENV_DETAIL_PROJECT" exists
      And "E2E_ENV_DETAIL_PROJECT" has an environment named "E2E_ENV_DETAIL"

    Scenario: The detail header shows the name, slug, status, and tab bar with Overview selected
      When the user opens the detail page of "E2E_ENV_DETAIL"
      Then the page heading should be "E2E_ENV_DETAIL"
      And the header should display the environment's slug and a status label
      And the tab bar should offer "Overview", "Folders", "Port forwards" and "Access"
      And the "Connect" link should be visible

    Scenario: The Overview tab shows usage vitals and the environment's configuration
      When the user opens the detail page of "E2E_ENV_DETAIL"
      Then the Overview tab should show "CPU", "Memory", "Disk" and "Last active" vitals
      And the Configuration section should show the environment name
      And the Configuration section should show the "Default (agent-server)" image
      And the Configuration section should show Docker access as "Disabled"
      And the idle timeout should default to 60 minutes

    Scenario: Switching tabs updates the URL hash
      When the user opens the detail page of "E2E_ENV_DETAIL"
      And the user clicks the "Access" tab
      Then the page URL should end with "#access"
      When the user clicks the "Port forwards" tab
      Then the page URL should end with "#portForwards"

    Scenario: A member with environments.write can edit the configuration
      Given the user has the "environments.read" and "environments.write" project permissions in "E2E_ENV_DETAIL_PROJECT"
      When the user opens the detail page of "E2E_ENV_DETAIL"
      Then the environment name and idle timeout fields should be enabled
      And the "Save changes" button should be visible but disabled until a field changes

    Scenario: A member without environments.write sees a read-only Overview
      Given the user has only the "environments.read" project permission in "E2E_ENV_DETAIL_PROJECT"
      When the user opens the detail page of "E2E_ENV_DETAIL"
      Then the environment name and idle timeout fields should be disabled
      And the "Save changes" button should not be visible

  @authenticated
  Rule: Deleting an environment

    Background:
      Given the user already has a stored authenticated session
      And a project named "E2E_ENV_DELETE_PROJECT" exists
      And "E2E_ENV_DELETE_PROJECT" has an environment named "E2E_ENV_TO_DELETE"
      And provisioning of "E2E_ENV_TO_DELETE" has settled
      And the user has opened the detail page of "E2E_ENV_TO_DELETE"

    Scenario: Cancelling the delete confirmation keeps the environment
      When the user opens the environment actions menu
      And the user selects "Delete"
      Then a delete confirmation dialog should appear naming "E2E_ENV_TO_DELETE"
      When the user cancels the delete confirmation
      Then the user should remain on the detail page of "E2E_ENV_TO_DELETE"

    Scenario: Confirming the deletion removes the environment and returns to the list
      When the user opens the environment actions menu
      And the user selects "Delete"
      And the user confirms the deletion
      Then the user should be returned to the Environments page
      And "E2E_ENV_TO_DELETE" should no longer appear on the Environments page
