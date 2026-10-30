@iam @git
Feature: Git permissions in the role editor
  The role dialog offers a switch for each Git action: "View Git", "Manage Git",
  "Approve change requests" and "Merge change requests" on a project role, and
  "Manage workspace Git connections" on a workspace role. Switching them on writes the
  actions the router gates on, on the resources they are checked on.

  Background:
    Given the user already has a stored authenticated admin session

  Scenario: A project role offers the four Git switches
    When the user opens the New Role dialog of a project
    Then the switches "View Git", "Manage Git", "Approve change requests" and "Merge change requests" are off

  Scenario Outline: Each project switch stores its action
    When "<switch>" is switched on and the role is created
    Then the stored role allows exactly "<action>" on "project/<id>/*"
    Examples:
      | switch                   | action      |
      | View Git                 | git:read    |
      | Manage Git               | git:write   |
      | Approve change requests  | git:approve |
      | Merge change requests    | git:merge   |

  Scenario: Several Git switches are stored together
    When "View Git" and "Manage Git" are switched on
    Then the role holds both actions

  Scenario: A workspace role offers Manage workspace Git connections
    When the user opens the Create Role dialog under Global Roles
    Then the switch "Manage workspace Git connections" is off
    When it is switched on and the role is created
    Then the stored role allows "git.identities:*" (the domain's only action) on "git_identity/*"

  Scenario: A stored Git role reopens in Simple with its switches on
    Given a project role allowing "git:read" and "git:write"
    When the user edits it
    Then the Simple tab is selected and "View Git" and "Manage Git" are on
