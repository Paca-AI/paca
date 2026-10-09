@iam @roles-assign
Feature: Assigning roles (roles:assign)
  Assigning is its own privilege and works like AWS IAM iam:PassRole. The
  resource of a roles:assign statement says which roles its holder may attach or
  detach: "project/<P>/role/<id>" inside a project, "role/<id>" platform-wide,
  "role/*" for every workspace role. Every role a request adds or removes is
  judged on its own resource; roles that stay as they are need nothing, and the
  holder does not need the permissions of the role they hand out.

  Rule: Assigning roles inside a project

    Background:
      Given the user already has a stored authenticated admin session
      And a project with roles Admin, Editor, Viewer and two custom roles "A" and "B"
      And "BOB" is a member holding Viewer

    Scenario: roles:assign alone, without project.members:write, changes a member's roles
      Given an assigner holding roles:assign on "project/<P>/role/*"
      Then the assigner can replace BOB's roles with Editor
      But removing BOB from the project answers 403

    Scenario: project.members:write alone assigns nothing; adding a member needs both permissions
      Then a holder of project.members:write only is refused when changing roles or adding a member
      And a holder of roles:assign only is refused when adding a member
      And a holder of both adds a member with 201

    Scenario: A new member needs at least one role
      When a member is added with an empty role list
      Then the API answers 400 ROLE_REQUIRED

    Scenario: Without roles:assign, a member cannot change anyone's roles
      Then an Editor changing BOB's roles gets 403 FORBIDDEN and nothing changes

    Scenario: A scoped roles:assign assigns exactly the roles it names, and removal is judged too
      Given a lead holding roles:assign on role "A" only
      Then adding A next to Viewer answers 200
      But adding B, swapping A for B, or adding Admin answers 403 FORBIDDEN and applies nothing
      When the admin also gives BOB role B
      Then the lead cannot drop B or empty the set
      But the lead can drop A while B stays

    Scenario: The project Admin can assign any role inside its project, but nothing platform-wide
      Then the project Admin assigns Editor, Admin, a pair of custom roles and a workspace role to BOB
      But assigning the same workspace role platform-wide answers 403

    Scenario: A project Admin can assign a richer role than it could grant by hand, yet it stays inside the project
      Given a workspace role with "*" on "*"
      When the project Admin attaches it to BOB inside the project
      Then BOB has full access in the project but cannot list workspace roles

    Scenario: Unknown role ids and roles of another project cannot be attached
      Then an unknown id and another project's role answer 422 ROLE_NOT_ATTACHABLE

  Rule: Assigning workspace roles to users and agents

    Background:
      Given the user already has a stored authenticated admin session

    Scenario: roles:assign on role/* assigns any workspace role to a user
      Then a holder of roles:assign on "role/*" can replace a user's roles

    Scenario: Without roles:assign a user cannot assign workspace roles
      Then a holder of users:write and roles:read is refused with 403 FORBIDDEN

    Scenario: A scoped role/<id> assigns that role only, and cannot remove another
      Then a holder of roles:assign on "role/<X>" assigns X but not Y
      And cannot take Y away, but may take X away while Y stays

    Scenario: A workspace-wide role/* does not reach inside a project
      Then a holder of roles:assign on "role/*" changing a project member's roles gets 403

    Scenario: A global agent's roles need agents:write on the agent as well as roles:assign
      Then roles:assign alone and agents:write alone are refused, both together succeed

  Rule: The Team page offers what the viewer may do

    Background:
      Given the user already has a stored authenticated admin session
      And a project with member "BOB" holding Viewer and custom roles "A" and "B"

    Scenario: The role editor is shown only to someone holding roles:assign
      Then a reader sees BOB's roles as badges and no "Change role" button
      And a holder of roles:assign sees "Change role" on the row

    Scenario: Add Member needs project.members:write and roles:assign together
      Then the "Add Member" button is hidden with either permission alone
      And visible with both, opening the Add member dialog

    Scenario: A scoped assigner can pick the role they may give, and is told when a role is not theirs
      When the lead picks role B for BOB
      Then an alert says they don't have permission to assign or remove one of these roles
      And BOB's roles are unchanged
      When the lead picks role A for BOB
      Then BOB holds Viewer and A

    Scenario: The project Admin can give any role of the project from the Team page
      When the Admin picks role B for BOB
      Then BOB holds Viewer and B

    @fixme
    Scenario: A lead whose roles:assign names specific roles still gets the role editor on the Team page
      # The permissions hint omits roles:assign when it is granted on specific
      # roles only, so the web app hides the editor. See the spec for the evidence.
      Given a lead holding roles:assign on "project/<P>/role/<A>" only
      Then the permissions endpoint lists "roles:assign"
      And the Team page shows "Change role" on BOB's row
