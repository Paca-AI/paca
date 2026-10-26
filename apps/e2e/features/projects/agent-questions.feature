@projects @agent-questions
Feature: Agent questions and mentions
  An agent can ask the user a multiple-choice question with the
  ask_user_question MCP tool. In a chat conversation the question is shown as
  a card under the tool call and the picked answer is sent as the user's next
  message. On a task or document it is posted as a comment carrying a
  `paca-ask` code block; the activity pane renders that comment as the same
  card and posts the answer as a reply that @-mentions the asking member, which
  is what wakes an agent. Plain-text @mentions in task and document comments
  (what agents write) are turned into real mention badges by the API, and a
  person mentioned in a document comment gets an in-app notification that
  opens the document.

  The e2e stack has no LLM credentials, so the agent side is simulated: the
  question comments are posted over the API (as the admin, so the answer's
  reply mentions the admin) and the chat transcript is supplied through a
  mocked events response.

  @authenticated
  Rule: Mentions written as plain text become mention badges

    Background:
      Given the admin is signed in
      And a project named "E2E_AQ_..." exists with a task and a document

    Scenario: "@username" in a task comment becomes a mention
      When a task comment "Thanks @admin, over to @nobody_here" is posted over the API
      Then the stored comment should contain a mention of the admin
      And the text "@nobody_here" should stay plain text

    Scenario: "@Name (id: ...)" in a task comment becomes a mention
      When a task comment "@someone (id: <the admin's member id>) please look" is posted over the API
      Then the stored comment should contain a mention of the admin

    Scenario: "@username" in a document comment becomes a mention
      When a document comment "Thanks @admin" is posted over the API
      Then the stored comment should contain a mention of the admin

    Scenario: E-mail addresses, inline code and code blocks are left alone
      When a task comment containing "ops@admin.example", inline code "@admin" and a code block "@admin" is posted
      Then none of them is converted to a mention
      And the e-mail address should stay in the stored text

  @authenticated
  Rule: A person mentioned in a document comment is notified

    Scenario: The mentioned member sees a notification that opens the document
      Given a project member "E2E_AQ_MEMBER_..." and a document in the project
      When the admin posts a document comment mentioning "@E2E_AQ_MEMBER_..."
      And the member signs in and opens the notification bell
      Then it should say "mentioned you in document <title>"
      And clicking it should open the document

  @authenticated
  Rule: A question in a comment is shown as a card

    The card is the same on a task and on a document (the document's
    "Comments & activity" panel), so every scenario runs for both targets.

    Background:
      Given the admin is signed in
      And a project named "E2E_AQ_..." exists with a task and a document

    Scenario Outline: The comment shows the question and its options, not the raw payload
      Given a <target> comment carrying a "paca-ask" question with options "Test 1", "Test 2", "Test 3" is posted over the API
      When the user opens the <target>
      Then the question and the three options should be visible
      And the raw JSON payload should not be shown

      Examples:
        | target   |
        | task     |
        | document |

    Scenario Outline: Picking an option posts a reply that mentions the asker
      Given a <target> comment carrying a "paca-ask" question with options "Test 1", "Test 2" is posted over the API
      When the user opens the <target> and clicks "Test 2"
      Then "You answered: Test 2" should be shown
      And a reply comment "Test 2" with a mention of the asker should be stored

      Examples:
        | target   |
        | task     |
        | document |

    Scenario Outline: A multi-select question collects several options
      Given a <target> comment carrying a question that allows several answers is posted over the API
      When the user opens the <target>, picks "Test 1" and "Test 3" and clicks "Send answer"
      Then "You answered: Test 1, Test 3" should be shown
      And a reply comment "Test 1, Test 3" should be stored

      Examples:
        | target   |
        | task     |
        | document |

    Scenario Outline: The user can type their own answer
      Given a <target> comment carrying a "paca-ask" question is posted over the API
      When the user opens the <target>, types "Something else" and clicks "Send answer"
      Then "You answered: Something else" should be shown
      And a reply comment "Something else" should be stored

      Examples:
        | target   |
        | task     |
        | document |

  @authenticated
  Rule: A question in a chat conversation is shown as a card

    Scenario: The card stays visible without expanding the tool calls and sends the answer as a message
      Given a conversation whose transcript ends with an ask_user_question call
      When the user opens the conversation
      Then the question and its options should be visible without expanding anything
      And clicking "Test 1" should send "Test 1" as a message in that conversation
