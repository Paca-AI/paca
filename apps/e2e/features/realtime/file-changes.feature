@realtime @file-changes
Feature: The file manager follows other people's changes
  Creating, renaming, moving, trashing or restoring a file or folder publishes
  a realtime-only `file.changed` event that carries ids, never names. A project's
  event reaches members holding files:read on the project; the general
  (workspace-wide) scope's event reaches accounts holding platform-level
  files:read. The web app refetches only the listings the event names.

  Rule: A project's file manager follows other members

    Scenario: A folder created by someone else appears without a reload

    Scenario: A renamed folder updates in place

    Scenario: A trashed folder disappears and a restored one comes back

    Scenario: A folder moved into another folder leaves the listing it was in

  Rule: The general scope follows other people's changes

    Scenario: A folder created in the general scope appears without a reload

  Rule: Who receives file.changed

    Scenario: A member with files:read receives the event, without any file name

    Scenario: A member without files:read receives nothing

    Scenario: An account with platform files:read receives a general-scope event

    Scenario: An account without platform files:read receives no general-scope event
