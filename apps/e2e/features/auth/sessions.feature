@auth
Feature: Signed-in devices
  An account can see every browser or device it is signed in on, and sign any
  of them out. Signing a device out ends it at once: its tokens stop working
  immediately, not when they next expire. Every session belongs to one
  account; nobody can see or end another account's.

  Rule: Seeing devices

    Scenario: The current device is listed and cannot be signed out from the list
      Given a user signed in on one device
      When they open the security page
      Then the signed-in devices list shows "This device"
      And it offers no sign-out button for that device
      And there is no "Sign out all other devices" button

    Scenario: Another device appears with its browser and system
      Given a user signed in on one device
      When they sign in again from Firefox on Windows
      Then the first device's list shows "Firefox on Windows"
      And the second device's list marks itself as "This device"

    Scenario: A device that signs out disappears from the list
      Given a user signed in on two devices
      When one of them signs out
      Then the other device's list no longer shows it

  Rule: Signing a device out

    Scenario: Signing out another device ends it immediately
      Given a user signed in on two devices
      When they sign out the other device from the list and confirm
      Then it leaves the list
      And the other device's very next request is rejected as unauthenticated
      And the other device is taken back to the sign-in page when it reloads
      And the current device keeps working

    Scenario: Cancelling the confirmation leaves the device signed in
      Given a user signed in on two devices
      When they choose to sign out the other device and then cancel
      Then it stays in the list
      And the other device keeps working

    Scenario: Signing out all other devices keeps only the current one
      Given a user signed in on three devices
      When they sign out all other devices and confirm
      Then only "This device" remains in the list
      And both other devices are rejected as unauthenticated
      And the current device keeps working

  Rule: Sessions are private to their account

    Scenario: Another account cannot end a session it does not own
      Given two users, each signed in
      When the second asks to end the first one's session by id
      Then the answer is "not found", exactly as for an id that does not exist
      And the first user's session keeps working
      And the first user's session id is not in the second user's list

    Scenario: A malformed session id is simply not found
      Given a signed-in user
      When they ask to end a session whose id is not a session id
      Then the answer is "not found", not a server error
