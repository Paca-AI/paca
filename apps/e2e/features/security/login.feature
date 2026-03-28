@security
Feature: Login security hardening
  The login form should reject malicious username payloads with a generic authentication error.

  Background:
    Given the browser starts with a clean unauthenticated session
    And the user is on the login page

  Scenario: SQL injection in the username is rejected
    When the user signs in with username "admin' OR '1'='1" and password "password"
    Then the generic invalid-credentials message should be visible
    And the user should remain on the login page

  Scenario: XSS payload in the username is rejected
    When the user signs in with username "<script>alert('xss')</script>" and password "password"
    Then the generic invalid-credentials message should be visible
    And the user should remain on the login page

  Scenario: LDAP injection in the username is rejected
    When the user signs in with username "*)(uid=*))(|(uid=*" and password "password"
    Then the generic invalid-credentials message should be visible
    And the user should remain on the login page

  Scenario: Directory traversal in the username is rejected
    When the user signs in with username "../../../etc/passwd" and password "password"
    Then the generic invalid-credentials message should be visible
    And the user should remain on the login page