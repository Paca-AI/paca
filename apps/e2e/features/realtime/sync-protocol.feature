@realtime @sync
Feature: The sync feed delivers every change in order, once, and only what the account may read
  A client that joins a project with "sync:join" receives the project's changes
  as "sync:changes" batches that say which sequence number they follow, can
  resume from its cursor after any absence, and is told to refetch only when
  the history cannot be replayed. The web app uses it behind a flag
  (localStorage "paca.realtime.sync"); the specs turn it on, or speak the
  protocol on a bare socket.

  Specs: tests/realtime/sync-protocol.spec.ts (basics), sync-entities.spec.ts,
  sync-authorization.spec.ts, sync-resilience.spec.ts, sync-ui.spec.ts, and
  live-updates-ui.spec.ts with E2E_SYNC=1.

  Scenario: Joining
    Given a member syncing a project
    Then the ack lists the rooms, the log's epoch and its head, and the mode "live"
    And a socket that syncs gets no legacy "event" for logged changes
    And a late joiner starts at the head and sees no history
    And joining twice on one socket delivers each change once

  Scenario: Every kind of change arrives with what a client needs
    Then a created task arrives as a create with the task, its people and its actor
    And an update carries the new task and the diff, and never the description
    And a delete arrives without an entity but names the task
    And a change too large to attach arrives without the entity
    And comments, links, sprints, views, docs, folders, task statuses, task
      types, custom fields, project renames, members, project agents and
      environments each arrive as their own change
    And an agent's edit with its own key arrives attributed to the agent

  Scenario: Authorization follows IAM
    Then what a role may not read arrives as a bare skip, keeping the sequence gapless
    And a Deny on one task, doc, sprint or view hides exactly that one
    And a role limited to one task still joins the tasks room and gets only that task
    And the task schema, members and agents need tasks:read, project.members:read and agents:read
    And a non-member, or an account with no rooms, gets no feed, not even skips
    And revoking, restoring, adding a Deny or attaching a role changes what the open socket gets
    And a replay is judged by what the account may read now

  Scenario: Order and gaplessness
    Then sixty sequential edits arrive in order, once each, in a chain of gapless batches
    And forty parallel edits and two concurrent writers leave no hole and no repeat
    And two projects on one socket keep separate numbering

  Scenario: Resuming
    Then five disconnect and reconnect cycles lose nothing and repeat nothing
    And a cursor equal to the head is live and replays nothing
    And a hundred and twenty missed changes arrive as one gapless replay
    And more than a thousand missed changes are answered with a reset
    And a cursor from another epoch, or ahead of the log, is answered with a reset
    And leaving stops the feed and joining again with the cursor replays the gap

  Scenario: The log is lost or trimmed
    Then a connected socket is sent a reset when the log is recreated, then follows the new log
    And a cursor older than what the log keeps is answered with a reset
    And the stream is bounded and expiring while its counter never expires

  Scenario: Malformed requests do no harm
    Then a cursor that is negative, fractional or not a number is handled
    And a join with no project, a non-string project or no callback is ignored
    And an injected project id is just an unknown project

  Scenario: Next to the legacy events and the token lifecycle
    Then a legacy socket gets the event and a syncing one the change, about the same task
    And notifications still reach a syncing socket
    And a syncing socket is asked to reauthenticate, keeps receiving, and is disconnected at expiry

  Scenario: The page follows other people's changes
    Then a title change patches the open table in place with no task list fetched
    And a change a list may filter or sort on refetches the list
    And a comment refreshes only the task's timeline, not its links, attachments or subtasks
    And tasks, sprints, docs, folders, members and their roles, the project name,
      a status and a view follow without a reload
    And two browsers, one on the plain protocol, follow the same change
    And losing and regaining tasks:read hides and shows the tasks
