@admin @settings @sso
Feature: Single sign-on (SSO) with OpenID Connect
  The "Single sign-on (SSO)" section of Admin > Settings (route
  /admin/settings) lets an administrator register OpenID Connect identity
  providers. It is gated by its own global permission, "settings.sso:write",
  separate from the branding permission "settings:write". Each enabled
  provider adds a "Continue with <name>" button to the sign-in page. A
  first-time user is given a new account with the default role (when the
  provider creates accounts automatically), and a returning user signs back
  in to the same account, matched by the provider's subject ("sub") claim.
  An optional email-domain allow-list turns away users whose verified email
  is on another domain, and sends them back to the sign-in page with an
  error.

  The E2E stack runs a mock OpenID Connect provider (mock-oauth2-server,
  service "mock-oidc") whose login form accepts any subject and claims. Each
  run registers its own issuer path and slug, so providers from parallel
  runs never collide, and removes its providers and accounts afterwards.

  Rule: Access is gated by the settings.sso:write permission

    Scenario: A user with only settings:write does not see the SSO section
      Given a user exists whose global role grants "settings:write" but not "settings.sso:write"
      When that user signs in and navigates to the Settings page
      Then the "Logo & Favicon" branding section should be displayed
      And the "Single sign-on (SSO)" section should not be displayed

    Scenario: A user with only settings.sso:write sees the SSO section but not branding
      Given a user exists whose global role grants "settings.sso:write" but not "settings:write"
      When that user signs in and navigates to the Settings page
      Then the "Single sign-on (SSO)" section should be displayed
      And the "Logo & Favicon" branding section should not be displayed

  Rule: An administrator manages providers, and users sign in with them

    Scenario: An administrator adds a provider
      Given the administrator is on the Settings page
      When they add a provider with a display name, slug, issuer URL, client ID and client secret
      Then the dialog should show the callback URL "<base URL>/api/v1/auth/sso/<slug>/callback"
      And the provider should be listed as "Enabled"

    Scenario: The sign-in page offers enabled providers
      Given an enabled provider exists
      When a signed-out visitor opens the sign-in page
      Then a "Continue with <provider name>" button should be displayed

    Scenario: A first-time user gets a new account
      Given an enabled provider that creates accounts automatically
      When a signed-out visitor continues with that provider
      And signs in at the provider with a new subject, a verified email and a preferred username
      Then the visitor should land on the Home page
      And be signed in to a new account named after the preferred username, with that email

    Scenario: A returning user signs back in to the same account
      Given a user has already signed in with the provider once
      When they continue with the provider again with the same subject
      Then they should be signed in to the same account as before

    Scenario: An email outside the allowed domains is refused
      Given the provider only allows emails on "corp.example"
      When a visitor signs in at the provider with a verified email on another domain
      Then they should be returned to the sign-in page
      And the error "Your email address isn't allowed to sign in here." should be displayed

    Scenario: A deleted provider disappears from the sign-in page
      Given the administrator deletes the provider on the Settings page
      When a signed-out visitor opens the sign-in page
      Then the "Continue with <provider name>" button should not be displayed
