@projects @git @automation
Feature: Git nodes in the automation graph
  Core provides the same Git triggers, condition and actions for every Git
  platform: pull request created or state changed, issue created, branch
  created, a pull request state / CI / draft / target branch condition, and
  merge / comment / close / label actions. The automation that used to be built
  on the GitHub plugin's own nodes is now built on these. The e2e stack has no
  Git provider, so the specs that run a graph put the linked pull request and
  the queued event in the stack's own database and queue.

  Background:
    Given the user already has a stored authenticated admin session
    And a project with a task

  Rule: The palette offers the Git nodes

    Scenario: Every Git node type is listed under its kind
      When the project's automation node types are fetched
      Then the triggers include "Git: Pull Request State Changed" and "Git: Issue Created"
      And the conditions include "Git: Pull Request State"
      And the actions include "Git: Merge Pull Request"
      And no node type is named "github.*"

    Scenario: The builder's Plugins section lists the Git triggers
      Given an empty automation
      When the user opens "Add Trigger"
      Then "Git: Pull Request State Changed" and "Git: Issue Created" are offered

    Scenario: Adding a Git trigger from the palette places it on the canvas
      Given an empty automation
      When the user adds "Git: Issue Created"
      Then it is on the canvas

  Rule: The graph is validated

    Scenario: The old GitHub plugin node types are refused
      When a trigger of type "github.pr_state_changed" is added
      Then it is refused as an invalid node type

    Scenario: An issue belongs to no task
      Given an automation with an "Git: Issue Created" trigger
      Then it may connect to a "wait" action
      But not to "update_task" or to a condition

    Scenario: A pull request trigger reaches task nodes
      Given an automation with a "Git: Pull Request State Changed" trigger
      Then it may connect to a "Git: Pull Request State" condition
      And that condition's "true" branch may connect to "update_task"
      And an edge from the condition without a branch is refused

  Rule: A graph runs when the event arrives

    Scenario: A merged pull request sets the task to another status
      Given a task linked to a merged pull request
      And an active automation: pull request state changed -> state is merged -> update the status
      When the state-changed event is queued
      Then the task gets the new status
      And the run is completed

    Scenario: A pull request that is not merged leaves the task alone
      Given a task linked to an open pull request
      When the same state-changed event is queued
      Then the task keeps its status
      And the run reports the condition as not matched

    Scenario: A new pull request runs its automation
      Given an active automation: pull request created -> update the status
      When the pull-request-created event is queued
      Then the task gets the new status

    Scenario: An opened issue runs a task-less automation
      Given an active automation: issue created -> wait
      When the issue-created event is queued with no task
      Then a run starts with the wait step recorded
