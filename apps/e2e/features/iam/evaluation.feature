@iam @evaluation
Feature: How a request is decided
  Paca decides every request from the roles of the principal that makes it: a
  user, or an agent acting as itself. Nothing is allowed by default, a matching
  Deny always wins over any Allow, the statements of all of a principal's roles
  add up, and an edit to a role or to someone's roles applies on their next
  request. Role names mean nothing: only the policy counts.

  These scenarios run over the API. Members sign in as themselves, so each
  request is judged against that user's own roles.

  Rule: Nothing is allowed by default

    Background:
      Given the user already has a stored authenticated admin session
      And a project named "E2E_IAMEVAL_PROJECT" with a task exists

    Scenario: A member whose role has no statements cannot read tasks
      Given a member holds only a project role with no statements
      Then listing tasks, opening the project and its sprints, and creating a task answer 403
      And the member's permissions in the project are an empty list

    Scenario: A member with no role at all sees nothing, and gets access back with a role
      Given a member whose roles were replaced with an empty set
      Then listing tasks answers 403
      When the member is given the Viewer role again
      Then the member lists the project's tasks

    Scenario: An action granted on one resource is not granted on another
      Given a member holds a role allowing "tasks:read" on their own project only
      Then the member can list tasks in that project
      But listing tasks in another project answers 403
      And creating a task in their own project answers 403

  Rule: An explicit Deny always wins

    Background:
      Given the user already has a stored authenticated admin session
      And a project named "E2E_IAMEVAL_PROJECT" with a task exists

    Scenario: A Deny beats an Allow held through another role
      Given a member holds the Editor role and a role denying "tasks:write" on the project
      Then creating, editing and deleting a task answer 403
      But the member can still list tasks and create a document

    Scenario: A Deny beats the project Admin's full access, but only where it points
      Given a member holds the Admin role and a role denying tasks on "project/<id>/task/*"
      Then opening one task answers 403 and the task list is empty
      But the member can still rename a sprint

  Rule: Roles add up and edits apply on the next request

    Background:
      Given the user already has a stored authenticated admin session
      And a project named "E2E_IAMEVAL_PROJECT" exists

    Scenario: Several roles on one member add up their permissions
      Given a role that reads tasks and a role that reads documents
      Then a member with the first role reaches tasks but not documents
      And a member with the second role reaches documents but not tasks
      And a member with both reaches both

    Scenario: Removing a role removes the access it gave
      Given a member holds the Editor role and can create tasks
      When the member's roles are replaced with Viewer
      Then the member can read but no longer create tasks
      When the member's roles are replaced with nothing
      Then the member can no longer read tasks

    Scenario: Editing or deleting a role changes what its holders may do at once
      Given a member holding a role that reads and writes tasks, signed in once
      When the role is edited to read only
      Then the same session can no longer create tasks
      When the role is deleted
      Then the same session can no longer list tasks

    Scenario: A role's name means nothing
      Given a project role called "ADMIN" that only reads documents
      Then its holder reads documents but not tasks and cannot list workspace roles

  Rule: An agent is judged by its own roles

    Background:
      Given the user already has a stored authenticated admin session

    Scenario: A global agent has the permissions of its own roles, not those of the bot behind the key
      Given a global agent that holds the default role
      Then, acting as the agent, its own permissions are "users:read"
      When the agent is given a role that only allows "plugins:read"
      Then its permissions are exactly "plugins:read" and listing users is refused
      And the same key without an agent id is the shared bot user with many more permissions

    Scenario: A project agent is judged by the roles it holds in the project
      Given a project agent that holds the Admin role
      Then, acting as the agent, it can list and create tasks
      When it holds only the Viewer role
      Then it can list tasks but creating one answers 403
      When it holds Admin and a role denying tasks
      Then it lists no tasks and creating one answers 403
      And a claimed agent id that does not exist is not trusted
