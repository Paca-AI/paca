@iam @role-badges
Feature: Role badges and the role selector
  A principal can hold any number of roles, so users, project members and agents
  show them as badges: a tinted pill whose tooltip carries the description, a
  "+N" overflow popover, and tags for Default, Built-in and Full access.
  Roles are chosen with a searchable multi-select: type to filter by name or
  description, pick several, see a selected count, Clear. A project member must
  keep at least one role.

  Setup is over the API; the browser checks what each page shows and does. The
  roles tab of global agents is covered by the agents feature.

  Rule: Role badges on the users list

    Background:
      Given the user already has a stored authenticated admin session
      And a user holds USER and three custom roles, one with a description
      And another user holds only USER
      And the users list is searched for the test prefix

    Scenario: The table has User, Role and Created columns and shows each user's roles
      Then the columns "User", "Role" and "Created" are visible
      And the plain user's row shows "USER"

    Scenario: Roles past the second fold into a +N badge that opens the full list
      Then the user with four roles shows two badges and a "+2" button named "Show more roles (+2)"
      And the single-role user has no overflow button
      When the "+2" button is clicked
      Then a popover "All roles (4)" lists all four role names

    Scenario: Hovering a badge shows the role's description and what kind of role it is
      When the pointer is on the described role's badge
      Then the tooltip shows its description
      When the pointer is on the USER badge
      Then the tooltip shows "Built-in" and "Default"

    Scenario: The role filter narrows the list to users holding a role, and Clear filters resets it
      When "Filter by role" is set to the described role
      Then only the user holding it remains and "Results: 1" is shown
      When "Clear filters" is clicked
      Then both users are listed again

  Rule: Choosing roles for a user

    Background:
      Given the user already has a stored authenticated admin session
      And three custom roles exist, one with a description
      And the "Change role" dialog is open for a user holding USER

    Scenario: Roles are listed with Current, Built-in, Default and Full access tags
      Then USER is described as Current, Built-in and Default
      And SUPER_ADMIN is described as Full access
      And an unheld role is not Current, and a described role shows its description

    Scenario: Typing filters the roles by name or description
      When part of a role name is typed
      Then only matching roles are listed
      When part of a description is typed
      Then the role with that description is listed
      When nothing matches
      Then "No roles match" is shown, and "Clear search" brings the list back

    Scenario: Several roles can be picked, counted and cleared
      Then "Selected: 1" is shown
      When two more roles are picked
      Then "Selected: 3" is shown and both options are selected
      When Clear is pressed
      Then "Selected: 0" is shown and the Clear button disappears

    Scenario: The chosen set is saved and shown as badges
      When two roles are added and "Assign role" is pressed
      Then the user holds USER and both roles

    Scenario: A role that grants everything is flagged before it is assigned
      When SUPER_ADMIN is ticked
      Then "This role has full access" is shown
      When it is unticked
      Then the warning goes away

  Rule: Role badges and the selector on the Team page

    Background:
      Given the user already has a stored authenticated admin session
      And a project exists

    Scenario: A reader sees up to three role badges and a +N popover for the rest
      Given a member holds four roles and a reader may only view the team
      Then the member's row shows three badges and a "Show more roles (+1)" button
      When it is clicked
      Then "All roles (4)" lists every role

    Scenario: A member keeps at least one role: the last one cannot be unpicked and there is no Clear
      Given a member holds only Viewer
      When the admin opens the member's role chip
      Then "At least one role is required." is shown and Viewer is selected and disabled
      And clicking Viewer does not unselect it, and there is no Clear button
      When Editor is added
      Then Viewer can be unpicked again, down to one role

    Scenario: The selector filters as you type, tags each role, and picks several at once
      When the admin opens a Viewer member's role chip
      Then Viewer is Current, and the project Admin is Built-in and Full access
      And typing "edit" leaves Editor only, and "zzz-nothing" shows "No roles match"
      When Editor and Admin are picked
      Then "Selected: 3" is shown and the member holds all three, with a "+1" on the row

    Scenario: Add member's role field is a searchable multi-select with a count and Clear
      When the admin opens the Add member dialog and its role field
      Then typing "view" leaves Viewer only
      When Viewer and Editor are picked
      Then "Selected: 2" is shown, Clear empties it, and "Add member" stays disabled
