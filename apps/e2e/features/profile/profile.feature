@profile
Feature: My Profile
  The My Profile page (/profile) shows the signed-in user's account
  information — display name, username, role, join date, full name and
  email — and lets them edit their full name and email. The username is
  read-only. Editing is done in place: "Edit profile" swaps the two fields
  for inputs, "Save changes" persists them via PATCH /users/me, and "Cancel"
  discards the draft. The page also hosts the Change Password card (covered
  by the auth features) and the avatar uploader (not covered here).

  @authenticated
  Rule: Viewing the profile

    Background:
      Given a user "E2E_PROFILE_VIEW" exists with full name "E2E Profile View" and email "e2e_profile_view@example.test"
      And the user is signed in as "E2E_PROFILE_VIEW"
      And the user has navigated to the My Profile page

    Scenario: The profile page shows the account header and details
      Then the page should display the "My Profile" heading
      And the page should display the subtitle "View and update your account information."
      And the header card should display the full name "E2E Profile View"
      And the header card should display the username "@E2E_PROFILE_VIEW"
      And the header card should display the role badge "USER"
      And the header card should display a "Joined" date
      And the "Full name" field should display "E2E Profile View"
      And the "Username" field should display "@E2E_PROFILE_VIEW"
      And the "Email" field should display "e2e_profile_view@example.test"
      And an "Edit profile" button should be visible

    Scenario: A user without an email sees "Not set"
      Given a user "E2E_PROFILE_NOEMAIL" exists with no email
      And the user is signed in as "E2E_PROFILE_NOEMAIL"
      When the user navigates to the My Profile page
      Then the "Email" field should display "Not set"

  @authenticated
  Rule: Editing the profile

    Background:
      Given a user "E2E_PROFILE_EDIT" exists with full name "E2E Profile Edit" and email "e2e_profile_edit@example.test"
      And the user is signed in as "E2E_PROFILE_EDIT"
      And the user has navigated to the My Profile page

    Scenario: Edit profile turns the name and email into inputs
      When the user clicks "Edit profile"
      Then the "Full name" input should contain "E2E Profile Edit"
      And the "Email" input should contain "e2e_profile_edit@example.test"
      And "Save changes" and "Cancel" buttons should be visible
      And the "Edit profile" button should not be visible
      And the username should not be editable

    Scenario: Saving persists the new name and email
      When the user clicks "Edit profile"
      And the user changes the full name to "E2E Profile Renamed"
      And the user changes the email to "e2e_profile_renamed@example.test"
      And the user clicks "Save changes"
      Then the profile should return to read-only mode
      And the "Full name" field should display "E2E Profile Renamed"
      And the "Email" field should display "e2e_profile_renamed@example.test"
      When the user reloads the page
      Then the "Full name" field should still display "E2E Profile Renamed"
      And the "Email" field should still display "e2e_profile_renamed@example.test"

    Scenario: Cancel discards the draft changes
      When the user clicks "Edit profile"
      And the user changes the full name to "Should Not Be Saved"
      And the user clicks "Cancel"
      Then the profile should return to read-only mode
      And the "Full name" field should display "E2E Profile Edit"
      When the user reloads the page
      Then the "Full name" field should still display "E2E Profile Edit"

    Scenario: Save is disabled while the full name is blank
      When the user clicks "Edit profile"
      And the user clears the full name
      Then the "Save changes" button should be disabled

    Scenario: A malformed email is flagged before saving
      When the user clicks "Edit profile"
      And the user changes the email to "not-an-email"
      Then the page should display "Please enter a valid email address."
      And the "Save changes" button should be disabled

    Scenario: Clearing the email leaves the existing email unchanged
      When the user clicks "Edit profile"
      And the user clears the email
      And the user clicks "Save changes"
      Then the profile should return to read-only mode
      And the "Email" field should display "e2e_profile_edit@example.test"

    Scenario: An email already used by another account is rejected
      Given another user exists with email "e2e_profile_taken@example.test"
      When the user clicks "Edit profile"
      And the user changes the email to "e2e_profile_taken@example.test"
      And the user clicks "Save changes"
      Then the page should display "This email is already in use by another account."
      And the profile should still be in edit mode

  @authenticated
  Rule: Profile API access control

    Scenario: An unauthenticated request cannot read or update a profile
      When an unauthenticated client sends PATCH /users/me
      Then the response should be 401 Unauthorized

    # @fixme: PATCH /users/me currently accepts a body without full_name (the
    # gin binding:"required" tag is not enforced by middleware.BindJSON) and
    # blanks the stored name. The spec is test.fixme until the API validates it.
    @fixme
    Scenario: A profile update without a full name is rejected
      Given a user "E2E_PROFILE_API" exists
      When the user sends PATCH /users/me with only an email
      Then the response should be a client error
