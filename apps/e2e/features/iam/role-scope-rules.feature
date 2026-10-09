@iam @role-scope-rules
Feature: Where a role lives decides where it can be used
  A project-owned role may only name its own project. A workspace role may name
  anything; one whose every resource has a wildcard project segment ("project/*")
  is a project template that attaches per project only. New projects start with
  Admin, Editor and Viewer. Built-in roles can be edited but not deleted, the
  default role cannot be deleted, the last full-access holder stays, role names
  follow a few rules, and a role can only be saved by someone who already holds
  what it grants (no privilege escalation).

  These are server rules with exact status and error codes, so the scenarios
  run over the API.

  Rule: A project-owned role may only name its own project

    Background:
      Given the user already has a stored authenticated admin session
      And projects "OWNED" and "OTHER" exist

    Scenario: Resources outside the project are refused with ROLE_POLICY_INVALID and an issue path
      When a role is created in OWNED naming "project/*", another project, another project's tasks, "*" or "user/*"
      Then each answers 422 ROLE_POLICY_INVALID with an issue at "statements[0].resources[0]"

    Scenario: The issue points at the offending resource among several
      When a role names OWNED and OTHER together
      Then the only issue is at "statements[0].resources[1]"

    Scenario: The project's own resource forms are accepted
      Then a role naming "project/<id>", "project/<id>/*", "project/<id>/task/*" or one of its sprints is created with 201 and owned by the project

    Scenario: Updating a role to name another project is refused, and so does validating it
      Then updating or validating a role with another project's resources reports ROLE_POLICY_INVALID
      And the workspace-level validator accepts the same policy

  Rule: Workspace roles and project templates

    Background:
      Given the user already has a stored authenticated admin session

    Scenario: A workspace role naming a project is accepted and attachable platform-wide, reaching only that project
      Then the role is created with 201, attached to a user platform-wide, and the user reaches that project only

    Scenario: A workspace template (project/*) cannot be attached platform-wide or made the default
      Then attaching it to a user and making it the default both answer 422 ROLE_NOT_ATTACHABLE
      And the default role is unchanged

    Scenario: A workspace template is attachable per project and then acts only there
      Then a member holding it reads tasks in that project but not in another

    Scenario: A role mixing project/* with a platform resource is not a template
      Then it can be attached platform-wide

  Rule: A new project's own roles

    Background:
      Given the user already has a stored authenticated admin session

    Scenario: A project starts with exactly Admin, Editor and Viewer, on its own resources
      Then the project's roles are Admin, Editor and Viewer, each owned by the project
      And every resource they name starts with "project/<id>", never a placeholder
      And Admin is a system role holding "*", while Editor and Viewer are not system roles

    Scenario: Viewer reads and Editor writes, as their policies say
      Then Viewer has tasks:read but not tasks:write, Editor has both, and neither has roles:assign
      And a Viewer member can list tasks but not create one

  Rule: Built-in roles can be edited but not deleted

    Background:
      Given the user already has a stored authenticated admin session

    Scenario: The workspace built-ins are system roles that refuse deletion
      Then deleting SUPER_ADMIN, ADMIN or USER answers 409 (ROLE_IS_SYSTEM, or ROLE_IS_DEFAULT for USER)

    Scenario: A workspace built-in can be edited, and the edit is kept
      When the ADMIN role's description is edited
      Then the edit is kept and the role is still a system role
      And the description is restored afterwards

    Scenario: A project's Admin role can be edited but not deleted; Editor and Viewer can be deleted
      Then editing the Admin role answers 200 and deleting it answers 409 ROLE_IS_SYSTEM
      And deleting Editor or Viewer answers 204

  Rule: The default role cannot be deleted

    Scenario: It cannot be deleted until another role is made the default
      Given a custom role is made the default
      Then deleting it answers 409 ROLE_IS_DEFAULT and exactly one role is the default
      When USER is made the default again
      Then the custom role can be deleted

  Rule: The last full-access holder stays

    Scenario: The only full-access account cannot be stripped of its role
      Given this account is the only holder of an unconditional "*" on "*"
      Then replacing its roles with none answers 409 ROLE_LAST_FULL_ACCESS
      And narrowing the SUPER_ADMIN role so it no longer grants "*" answers 409 ROLE_LAST_FULL_ACCESS
      # Skipped when another account holds full access: stripping a non-last holder succeeds.

  Rule: Role names

    Background:
      Given the user already has a stored authenticated admin session

    Scenario: A name is trimmed and must be 1 to 100 characters
      Then an empty, blank or 101-character name answers 400 ROLE_NAME_INVALID
      And a padded name is stored trimmed and a 100-character name is accepted

    Scenario: A name is unique within its scope: workspace-wide and per project
      Then a duplicate workspace role name or duplicate name in one project answers 409 ROLE_NAME_TAKEN
      But the same name is free in another project and as a workspace role
      And "Editor" is taken in every project

    Scenario: A role is renamed over an existing name only when the name is free
      Then renaming to another role's name answers 409 ROLE_NAME_TAKEN
      And keeping its own name, or choosing a free one, answers 200

    Scenario: A permission map is not a policy
      Then a body with a permission map instead of a policy answers 422

  Rule: A policy is validated when a role is saved

    Background:
      Given the user already has a stored authenticated admin session

    Scenario: An unknown action is reported at its path
      Then "tasks:reed" answers 422 ROLE_POLICY_INVALID with an issue at "statements[0].actions[0]"

    Scenario: Bad resources, effects and operators are reported
      Then an unknown resource root, an empty segment and an unknown operator are reported at their paths

    Scenario: A condition key tied to another kind of resource is refused
      Then using "task.sprint_id" on a document statement answers 422 ROLE_POLICY_INVALID

  Rule: No privilege escalation when defining roles

    Background:
      Given the user already has a stored authenticated admin session

    Scenario: A workspace role can only be saved when its author holds what it grants
      Given an author holding roles:write and users:read
      Then they can create and update a role granting users:read
      But granting users:write, "*", roles:assign or users:read on "*" answers 403 FORBIDDEN
      And a Deny-only role is always accepted

    Scenario: A Deny of the author overlapping the grant makes it ungrantable
      Given an author whose own Deny covers one user
      Then granting users:read on "user/*" answers 403
      But granting it on an unrelated user resource answers 201

    Scenario: Inside a project the same guard applies to project roles
      Given a project member holding roles:write, tasks:read and a conditional docs:read
      Then they can create a role with tasks:read and a Deny-only role
      But tasks:write, agents:read and an unconditional docs:read answer 403
      And a project Admin can create any role inside the project
