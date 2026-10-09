@iam @docs
Feature: The worked examples of the roles and policies guide work as written
  Each example's JSON is read from docs/guides/roles-and-policies.md, its
  placeholders are replaced with real ids, the role is created over the API and
  what the guide promises is asserted. If an example in the guide is edited and
  breaks, these scenarios fail.

  Background:
    Given the user already has a stored authenticated admin session

  Rule: Limit tasks, sprint and views to one sprint

    Background:
      Given a project with a contractor holding the guide's sprint-limited role for sprint S1
      And S1 has three tasks, S2 two, and one task is in the backlog

    Scenario: Task lists return only that sprint's tasks, filtered by the database so pages and counts are right
      Then two pages of size 2 return the three tasks with a total of 3

    Scenario: Opening another sprint's task is refused, and a task can neither be created without the sprint nor moved out of it
      Then opening an S2 task answers 403 and creating a task without a sprint answers 403
      And moving a task between S1 and S2 answers 403 in both directions

    Scenario: Views of that sprint are listed, the other sprint's lists are empty and its views cannot be opened
      Then S1's views are listed, S2's list is empty and S2's and the backlog's views answer 403

    Scenario: Adding views:write to the statement lets the member manage that sprint's views only
      Given the guide's role with views:write added
      Then the member creates, renames and deletes views of S1 but not of S2, the backlog or a mixed reorder

  Rule: The same pattern limits documents to a folder and its subfolders

    Scenario: The guide's doc.ancestor_folder_ids statement, with the docs:read on the project it asks for
      Then a member lists the documents of the folder and its subfolder only

  Rule: A workspace role that gives access to two projects

    Scenario: It attaches platform-wide and reaches exactly those two projects
      Then a user holding it reads tasks in both projects but not in a third

    Scenario: The same policy on a project's own Roles page is refused at statements[0].resources[1]
      Then creating it as a project role answers 422 ROLE_POLICY_INVALID with that single issue

  Rule: Developer in one project

    Scenario: Attached platform-wide it gives the same result as attached in the project: that project and no other
      Then the user works in that project and gets 403 in another

  Rule: Restrict an agent

    Scenario: The Deny role hides the agent from whoever holds it
      Then the holder's agent list is empty and the agent's detail answers 403, while another member still sees it

    Scenario: The 'only these people' form denies everyone except the listed users
      Then the listed user reads the agent and the other member is refused

  Rule: Let project leads assign only Editor and Viewer

    Scenario: The lead assigns Editor and Viewer; Admin, in either direction, is refused with 403
      Then the lead assigns Editor and Viewer and adds a member with Editor
      But assigning Admin, taking Admin away from someone, or adding a member as Admin answers 403

  Rule: Let a support admin assign any workspace role

    Scenario: The policy lets its holder assign any workspace role without being able to edit roles
      Then the holder assigns ADMIN to a user and reads roles but creating a role answers 403
      And simulating "roles:assign" on the SUPER_ADMIN role against the policy is allowed

  Rule: Let someone assign any role in one project

    Scenario: The holder assigns any role of the project, the Admin role included, and manages members
      Then the holder gives and takes back the Admin role and adds a member as Admin
