@iam @role-editor
Feature: The role editor
  The role dialog (a project's Roles page, and Administration, Global Roles)
  edits one policy in two views. Simple is a switch per permission, including
  "View Project" and "Assign Roles". Advanced (JSON) validates as you type,
  simulates requests and suggests actions and condition keys. A role opens in
  Advanced, and stays JSON-only, when the switches cannot express it: a Deny,
  conditions, specific resources or actions the switches do not offer. Several
  plain Allow statements are the union of their actions, so they open in Simple.
  There is no "Limit to" control: scope is written in the policy.

  Rule: The Simple view

    Background:
      Given the user already has a stored authenticated admin session

    Scenario: A project role offers View Project and Assign Roles next to the other switches
      When the user opens the New Role dialog of a project
      Then the Simple tab is selected and every switch is off
      And the switches include "View Project", "Assign Roles" and "View Tasks"
      When "View Project" and "Assign Roles" are switched on and the role is created
      Then the stored role has one Allow statement with "projects:read" and "roles:assign" on "project/<id>/*"

    Scenario: A workspace role offers Assign Global Roles and writes the workspace resources
      When the user opens the Create Role dialog under Global Roles and switches on "Assign Global Roles"
      Then the stored role allows "roles:assign" on the workspace role resources

    Scenario: A role of several plain Allow statements opens in Simple with the union switched on
      Given a project role with three plain Allow statements
      When the user edits it
      Then the Simple tab is selected, without the advanced notice
      And the switches of the union are on
      When it is saved untouched
      Then every action is kept (tasks:read and tasks:write as "tasks:*")

  Rule: Roles that open in Advanced

    Background:
      Given the user already has a stored authenticated admin session

    Scenario Outline: A role with <feature> opens in Advanced with the notice, and switching back is refused
      Given a project role with <feature>
      When the user edits it
      Then the Advanced tab is selected and no switches are shown
      And the notice says "This role uses advanced features and can only be edited as JSON." and "<reason>"
      When the user clicks the Simple tab
      Then the Advanced tab stays selected with the same reason
      When the role is saved untouched
      Then its policy is unchanged

      Examples:
        | feature             | reason                             |
        | a Deny statement    | It has a Deny statement.           |
        | conditions          | It has conditions.                 |
        | specific resources  | It applies to specific resources.  |

  Rule: The Advanced view

    Background:
      Given the user already has a stored authenticated admin session
      And the user opens the New Role dialog of a project on the Advanced tab

    Scenario: Problems are listed by path as the policy is typed, and block saving until fixed
      When an unknown action is typed
      Then an alert lists "statements[0].actions[0]" with "unknown action" and Create role is disabled
      When a resource outside the project is typed
      Then the alert lists "statements[0].resources[0]"
      When a valid policy is typed
      Then the alert goes away and Create role is enabled

    Scenario: Broken JSON is reported and blocks saving
      When "{ \"statements\": [" is typed
      Then an alert says "The JSON is not valid" and Create role is disabled

    Scenario: A role is created straight from JSON, Deny and conditions kept as typed
      When a Deny statement and a conditional Allow are typed in a workspace role
      Then the stored role keeps both (a single condition value is stored as a list)

    Scenario: Simulate says whether a request is allowed and which statement decided
      Given a policy with a sprint-conditioned Allow "OnlyThisSprint" and a Deny "NoWrites"
      When "tasks:read" on a task is simulated with the sprint attribute
      Then the result is Allowed, "Decided by OnlyThisSprint (Allow)."
      When the attribute is left out
      Then the result is Denied: "No statement allows this request, so it is denied by default."
      When "tasks:write" is simulated
      Then the result is Denied, "Decided by NoWrites (Deny)."
      When an attribute line is not key=value
      Then the panel reports "Not a key=value pair"

    Scenario: Typing an action or a condition key offers suggestions that complete it
      When "tasks:wr" is typed inside an actions list
      Then the chip "tasks:write" is offered and completes the action
      When "task.sp" is typed inside a condition
      Then the chip "task.sprint_id" is offered and completes the key

    Scenario: A link to the IAM roles guide opens in a new tab
      Then the link "Read the IAM roles guide" points at "docs/guides/iam-authorization.md" with target _blank and rel noopener
      And the Simple view has no such link

  Rule: Switching between Simple and Advanced

    Background:
      Given the user already has a stored authenticated admin session

    Scenario: The switches become a policy, and an edited policy comes back as switches
      When "View Tasks" and "View Project" are switched on and the Advanced tab is opened
      Then the JSON has one Allow statement with those two actions
      When "docs:read" is added in the JSON and the Simple tab is opened
      Then "View Documents" is on as well
      When the role is created
      Then the stored actions are "docs:read", "projects:read" and "tasks:read"

    Scenario: JSON the switches cannot show stays JSON, with the reason
      When a Deny-only policy is typed and the Simple tab is clicked
      Then the Advanced tab stays selected and the notice says "It has a Deny statement."
      When broken JSON is typed and the Simple tab is clicked
      Then the notice says "The JSON is not valid yet."

  Rule: Name, description and scope

    Background:
      Given the user already has a stored authenticated admin session

    Scenario: The description has a line of its own and is saved and listed in the table
      Then the Description field sits below the Role Name field, marked "(optional)"
      When a description is entered and the role is created
      Then the table's Description column shows it, it is stored, and the edit dialog shows it again

    Scenario: There is no Limit-to control: scope is written in the policy
      Then neither view of the project or workspace dialog has any "Limit to" text, label or selector

    Scenario: The dialog says whether it edits a workspace or a project role
      Then the project dialog shows a "Project role" tag and the workspace dialog "Workspace role"

    Scenario: A name that is taken is refused inline, in a project and in the workspace
      When "Editor" is entered as a project role name and created
      Then "A role with this name already exists." is shown and the dialog stays open
      When an existing workspace role name is entered and created
      Then the same message is shown, and renaming clears it
