@projects @git
Feature: Linking, unlinking and automating Git in a project's settings
  With a linked repository the Git tab shows each repository with the bot to
  mention, lets a writer unlink it, and unlocks the Automation tab: the
  default responder, the minimum repository permission, and a pause switch,
  saved to the project's real settings. Link errors from the platform are
  explained in words. The provider and repository endpoints are stubbed in the
  browser because the e2e stack has no Git provider plugin; settings are real.

  Background:
    Given the user already has a stored authenticated admin session
    And a project with an agent
    And the platform lists a Git provider

  Rule: Linked repositories

    Scenario: A linked repository shows its branch and the bot to mention
      Given the project has "acme/web" linked, served by "paca-bot"
      Then the repository is listed with its default branch "main" and "@paca-bot"
      And the "Using it" tab shows the mention to type

    Scenario: Unlinking asks for confirmation
      Given the project has "acme/web" linked
      When the user clicks "Unlink"
      Then "Confirm unlink" and "Cancel" appear
      When the user cancels
      Then the repository is still listed
      When the user clicks "Unlink" and "Confirm unlink"
      Then the platform is asked to delete that repository link
      And the repository disappears from the list

    Scenario Outline: Link errors are explained
      When linking answers <code>
      Then the alert says "<text>"
      Examples:
        | code                     | text                                      |
        | GIT_ALREADY_LINKED       | already linked to the project             |
        | GIT_NO_IDENTITY          | No shared Git identity is set up          |
        | GIT_PROVIDER_UNAVAILABLE | provider plugin is unavailable            |
        | GIT_NOT_FOUND            | Not found                                 |

    Scenario: A repository the identity cannot see offers the install
      When linking answers GIT_ACCESS_NOT_GRANTED for "other-org/lib"
      Then the user is told the shared identity can't see it
      And a button "Install on other-org" is offered

  Rule: Automation

    Scenario: Automation is locked until a repository is linked
      Given the project has no repository
      Then the Automation tab says to link a repository first
      And its fields are disabled and there is no Save button

    Scenario: A missing default responder is warned about
      Given the project has "acme/web" linked
      Then a warning says mentions that name no agent are ignored

    Scenario: Choosing a responder saves it to the project
      Given the project has "acme/web" linked
      When the user picks the agent and clicks "Save"
      Then "Saved." is shown and the warning disappears
      And the project's settings name that agent

    Scenario: Minimum access and the pause switch are saved
      Given the project has "acme/web" linked
      When the user opens Advanced, picks "Admin" and switches on "Pause automation"
      And clicks "Save"
      Then the project's settings say paused with minimum permission "admin"
      And after a reload the same values are shown

    Scenario: Save is available only when something changed
      Given the project has "acme/web" linked
      Then "Save" is disabled until a field changes

    Scenario: A reader sees the settings but cannot change them
      Given a member with only "git:read"
      Then the fields are disabled and there is no Save button
