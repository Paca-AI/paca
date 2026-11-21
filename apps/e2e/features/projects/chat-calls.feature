@projects @chat @calls
Feature: Voice channel calls
  A voice channel is a standing LiveKit room people drop into, with their
  microphone and camera. Who is in it comes from LiveKit's webhooks, and
  a moderator can remove someone. The e2e stack runs LiveKit by default; the
  specs skip themselves when calls are off.

  Background:
    Given a project with a voice channel, an admin signed in and a member who may join calls

  Scenario: Two people talk in their own call tabs, one is removed by a moderator, both leave
    When both join the voice channel
    Then each sees the other in the call and the API lists both participants
    When the admin removes the member
    Then the member's call ends and only the admin remains
    When the admin leaves
    Then nobody is in the call

  Scenario: A member without calls:join is offered no way in
    Given a member who can read the channel but not join calls
    When the member opens the channel
    Then there is no Join call button
