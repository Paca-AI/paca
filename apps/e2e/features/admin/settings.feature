@admin @settings
Feature: Workspace branding settings
  The Settings page (Admin > Settings, route /admin/settings) is titled
  "Workspace Branding" and lets an administrator customise the whole
  instance: a logo and favicon, a brand name, and a primary accent colour
  chosen from eight curated light/dark presets (Green, Blue, Teal, Indigo,
  Purple, Pink, Red, Orange). The page is gated by the global "settings.write"
  permission: users without it are redirected to the home page. The form
  tracks unsaved changes — "Save changes" stays disabled until the brand name
  or colour differs from what is stored — and a successful save (PATCH
  /api/v1/admin/settings) shows a transient "Saved" confirmation. Branding is
  instance-wide state, so the scenarios below restore the original values
  afterwards. Logo and favicon uploads are not covered here.

  @authenticated
  Rule: Access is gated by the settings.write permission

    Background:
      Given the user already has a stored authenticated session

    Scenario: A user without settings.write is redirected away from the Settings page
      Given a user exists whose global role does not grant "settings.write"
      When that user signs in and navigates to the Settings page
      Then the user should be redirected to the Home page
      And the "Workspace Branding" heading should not be displayed

    Scenario: A user with only settings.write can open the Settings page
      Given a user exists whose global role grants "settings.write"
      When that user signs in and navigates to the Settings page
      Then the "Workspace Branding" heading should be displayed
      And the "Brand Name" field should be displayed

  @authenticated
  Rule: Branding form

    Background:
      Given the user already has a stored authenticated session
      And the workspace has no brand name or primary colour override
      And the user has navigated to the Settings page

    Scenario: The Settings page shows the branding form with nothing selected
      Then the page should display the heading "Workspace Branding"
      And the "Brand Name" field should be empty
      And none of the eight colour presets should be selected
      And the "Save changes" button should be disabled

    Scenario: Editing the brand name enables the Save button
      When the user types a new brand name into the "Brand Name" field
      Then the "Save changes" button should be enabled

    Scenario: Restoring the stored brand name disables the Save button again
      Given the workspace brand name is stored as "Acme"
      And the user has reloaded the Settings page
      And the user has changed the "Brand Name" field to "Acme Corp"
      When the user types "Acme" back into the "Brand Name" field
      Then the "Save changes" button should be disabled

    Scenario: Selecting a colour preset marks only that preset as selected
      When the user selects the "Blue" colour preset
      Then the "Blue" preset should be pressed
      And the other seven presets should not be pressed
      And the "Save changes" button should be enabled

    Scenario: Selecting another preset moves the selection
      Given the user has selected the "Green" colour preset
      When the user selects the "Red" colour preset
      Then the "Red" preset should be pressed
      And the "Green" preset should not be pressed

    Scenario: Saving persists the brand name and colour and shows a confirmation
      When the user types a new brand name into the "Brand Name" field
      And the user selects the "Purple" colour preset
      And the user clicks "Save changes"
      Then a PATCH request should be sent to "/api/v1/admin/settings" with the brand name and the Purple light/dark colours
      And a "Saved" confirmation should appear
      And the "Save changes" button should be disabled again
      And the public branding endpoint should return the new brand name and colours

    Scenario: Saved branding is still shown after reloading the page
      Given the user has saved a new brand name and the "Teal" colour preset
      When the user reloads the Settings page
      Then the "Brand Name" field should contain the saved brand name
      And the "Teal" preset should be pressed

    Scenario: A failed save shows an error and keeps the form editable
      Given the settings update request will fail with a server error
      When the user types a new brand name into the "Brand Name" field
      And the user clicks "Save changes"
      Then the page should display "Failed to save. Please try again."
      And no "Saved" confirmation should appear
      And the "Save changes" button should be enabled
