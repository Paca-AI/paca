@auth
Feature: Two-step verification and login security
  Accounts can add a TOTP authenticator app as a second step. With it on,
  the password alone only yields a short-lived challenge; a code (or a
  single-use recovery code) turns it into a session. Admins can limit where
  an account may sign in from, reset someone's two-step verification, and
  require it for chosen roles. Wrong codes and wrong passwords are throttled.

  Rule: Enrolling

    Scenario: Enrollment takes effect only once a valid code confirms it
      Given a user has started two-step verification setup
      When they confirm with a wrong code
      Then the code is rejected and sign-in is still password-only
      When they confirm with the current code
      Then 8 recovery codes are returned and two-step verification is on
      And starting setup again is refused

  Rule: Signing in with a second factor

    Scenario: A correct password alone yields a challenge, not a session
      Given a user with two-step verification on
      When they sign in with only the password
      Then no session is started and a challenge is returned
      And a wrong code is rejected
      And the current code starts the session

    Scenario: A challenge is not a session and works once
      Then the challenge is rejected as a bearer token
      And a second sign-in cannot reuse the same challenge

    Scenario: A used code is rejected
      Then submitting the same code for a second sign-in fails

    Scenario: A recovery code works once
      Then a recovery code starts a session and one fewer remains
      And the same recovery code fails the second time

    Scenario: Five wrong codes lock the second factor
      Then the fifth wrong code answers "too many requests" with a wait
      And the correct code is refused during the lockout
      When an admin resets the user's two-step verification
      Then the user signs in with the password alone again

  Rule: Managing your own two-step verification

    Scenario: Disabling and regenerating recovery codes need a current code
      Then regenerating with a wrong code fails
      And regenerating with a current code replaces every recovery code
      And an old recovery code cannot turn two-step verification off
      And a new recovery code turns it off

  Rule: IP allow-lists

    Scenario: An account restricted to other addresses cannot sign in
      Given an admin limits a user to 192.0.2.0/24
      Then the correct password is refused as "address not allowed"
      And a wrong password still reads as invalid credentials
      And an invalid entry is rejected
      And opening the list again lets the user in

    Scenario: An admin cannot lock themselves out
      Then saving their own allow-list without their address is refused
      And saving an instance allow-list without their address is refused

  Rule: Requiring two-step verification

    Scenario: Protected roles must set it up before signing in
      Given the policy requires two-step verification for one role
      Then a user outside the role signs in normally and cannot use password-only setup
      And a user with the role is told setup is required
      When they enroll with their password and confirm a code
      Then they sign in with a code and cannot redo setup with the password

  Rule: Who may change login security

    Scenario: Ordinary admins cannot read or change the policy
      Then the policy, a user's allowed addresses and a user's reset are all forbidden

  Rule: Repeated wrong passwords

    Scenario: Ten wrong passwords lock the sign-in, for unknown names too
      Then the tenth wrong password is answered "too many requests"
      And the correct password is refused while locked
      And a name that does not exist locks the same way

  Rule: In the browser

    Scenario: A user turns two-step verification on, signs in with it, and turns it off
      Then the setup dialog explains a wrong code
      And the right code shows the recovery codes once
      And signing in again asks for a code on its own page
      And a wrong code is explained and the right one opens the app
      And a recovery code turns two-step verification off

    Scenario: A recovery code signs in from the code page

    Scenario: A protected user is walked through setup on the sign-in page

    Scenario: An admin edits a user's allowed addresses and resets their two-step verification
      Then an invalid entry is explained
      And the saved list is applied to that user
      And the reset confirmation is shown

    Scenario: The login security settings explain a list that would lock the admin out
      Then the lock-out warning appears and saving is refused with a clear message
