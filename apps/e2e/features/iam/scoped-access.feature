@iam @scoped-access
Feature: Roles limited to part of a project
  A role does not have to open a whole project. Conditions and resource paths
  narrow it to the tasks or views of one sprint, one sprint by its id, the
  documents of a folder and its subfolders, or everything except one agent or
  environment. List endpoints apply that scope inside the query (before
  pagination and counts); a request that names something outside the scope is
  refused with 403, and creating or moving something is judged on the
  attributes the request carries.

  The admin creates the content over the API; the limited member signs in as
  themselves and every request is judged against their own roles.

  Rule: Tasks limited to one sprint (task.sprint_id)

    Background:
      Given the user already has a stored authenticated admin session
      And a project with sprints "S1" and "S2", three tasks in S1, two in S2 and one in the backlog
      And a member holds a role allowing tasks only where "task.sprint_id" is S1

    Scenario: The task list returns only that sprint's tasks, with matching totals
      Then the member lists exactly the three tasks of S1, with a total of 3
      And filtering by sprint S2 or by the backlog returns nothing, with a total of 0

    Scenario: Opening a task of another sprint, or of no sprint, is refused
      Then opening a task of S1 answers 200, by id and by task number
      But opening a task of S2 or a backlog task answers 403

    Scenario: Creating a task needs the sprint
      Then creating a task in S1 answers 201
      But creating one in S2, or without a sprint, answers 403

    Scenario: A task can neither be moved out of the sprint nor into another one
      Then renaming a task of S1 answers 200
      But moving it to S2 or to the backlog answers 403
      And moving a task of S2 or of the backlog into S1 answers 403
      And editing a task of S2 answers 403
      And no task has moved

    Scenario: Deleting is limited the same way
      Then deleting a task of S2 or a backlog task answers 403
      And deleting a task of S1 succeeds and the member's total drops to 2

    Scenario: The permissions hint still lists tasks, because some tasks are allowed
      Then the member's permissions in the project include "tasks:read" and "tasks:write"

  Rule: Views limited to one sprint (view.sprint_id)

    Background:
      Given the user already has a stored authenticated admin session
      And a project with two sprints, each with its own views
      And a member holds a role allowing views only where "view.sprint_id" is the first sprint

    Scenario: Lists return the sprint's views; other sprints and the backlog are empty
      Then the first sprint's view list is complete
      And the second sprint's, the backlog's and the timeline's lists are empty

    Scenario: A view of another sprint, or a backlog view, cannot be opened or changed
      Then reading or renaming a view of the first sprint answers 200
      But reading a view of the second sprint or a backlog view answers 403
      And renaming a view of the second sprint answers 403

    Scenario: Views are created in the sprint only
      Then creating a view in the first sprint answers 201
      But creating one in the second sprint, in the backlog or in the timeline answers 403

    Scenario: Reordering is refused when any listed view is not allowed
      Then reordering the first sprint's views answers 204
      But reordering the second sprint's views, or a mix of both, answers 403

    Scenario: Deleting a view is limited to the sprint
      Then deleting a view of the second sprint answers 403
      And deleting a view of the first sprint answers 204

    Scenario: A negated condition includes the project-level views too
      Given a role allowing views where "view.sprint_id" is not the second sprint
      Then the backlog views and the first sprint's views are listed
      And the second sprint's list is empty

  Rule: Sprints limited by resource id

    Scenario: Only that sprint is listed, readable and writable
      Given a member holds sprints:read and sprints:write on one sprint's resource only
      Then the sprint list contains that sprint alone
      And reading or patching it answers 200
      But reading, patching or deleting another sprint answers 403
      And creating a sprint answers 403

  Rule: Documents limited to a folder and its subfolders (doc.ancestor_folder_ids)

    Background:
      Given the user already has a stored authenticated admin session
      And folders "Handbook", "Handbook/Chapters" and "Private" with a document in each, and one at the root
      And a member holds a role allowing documents whose "doc.ancestor_folder_ids" contain "Handbook"

    Scenario: The folder and its subfolders are listed; everything else is left out
      Then the member lists the documents of Handbook and Chapters only

    Scenario: A document outside the folder cannot be opened or edited
      Then reading or editing the documents of Handbook and Chapters answers 200
      But reading the documents of Private or at the root answers 403
      And editing or deleting a document of Private answers 403

    Scenario: A document is created in the folder or a subfolder, nowhere else
      Then creating a document in Handbook or in Chapters answers 201
      But creating one in Private or without a folder answers 403

    Scenario: A document cannot be moved out of the folder
      Then moving a document to Private or to the root answers 403
      And moving it into a subfolder answers 200

  Rule: Denying one agent and one environment narrows the lists

    Background:
      Given the user already has a stored authenticated admin session
      And a project with agents "SECRET_BOT" and "OPEN_BOT" and environments "SECRET_ENV" and "OPEN_ENV"
      And "RESTRICTED" and "UNAFFECTED" are members with the Editor role
      And "RESTRICTED" also holds a role denying "agents:*" and "conversations:*" on SECRET_BOT and "environments:*" on SECRET_ENV

    Scenario: Both lists leave the denied resource out and keep the rest
      Then "RESTRICTED" lists only OPEN_BOT and OPEN_ENV
      And "UNAFFECTED" lists both agents and both environments

    Scenario: Routes on the denied resources answer 403, the others 200
      Then the detail of SECRET_BOT and SECRET_ENV answers 403 for "RESTRICTED"
      And the detail of OPEN_BOT and OPEN_ENV answers 200

    Scenario: Unrelated lists are untouched by the Deny
      Then "RESTRICTED" still lists the project's tasks and sprints
