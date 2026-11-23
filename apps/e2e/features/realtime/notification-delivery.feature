@realtime @notifications
Feature: A notification always reaches the bell
  A new notification is announced over the realtime socket, which keeps nothing
  for a client that is not connected. The notification itself is stored, so the
  web app re-reads the inbox from the API whenever it may have missed the
  announcement: when its socket reconnects, and once a minute while the tab is
  visible.

  Specs: tests/realtime/notification-delivery.spec.ts. The restart scenario
  stops the shared stack's realtime service, so it only runs with
  E2E_ALLOW_STACK_RESTART=1 and nothing else running against the stack.

  Scenario: Assigned while the browser is offline
    Given a member is signed in with the app open and the browser goes offline
    When a task is assigned to them
    And the browser comes back online
    Then the bell shows one unread notification

  Scenario: Assigned while the realtime service is down
    Given a member is signed in with the app open
    When the realtime service is stopped and a task is assigned to them
    And the realtime service is started again
    Then the bell shows one unread notification

  Scenario: The announcement is lost but the socket never drops
    Given a member is signed in with the app open and announcements are swallowed
    When a task is assigned to them
    Then the bell has no badge at first
    And within about a minute the bell shows one unread notification
