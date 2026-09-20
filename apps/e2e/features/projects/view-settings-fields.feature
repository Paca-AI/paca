@projects @interactions @views @settings @fields
Feature: View settings — field visibility on Board and List views
  A user can choose which task fields (built-in and custom) appear on Board
  cards and as List columns through the "Fields" picker inside the View
  settings panel.  "Title" is always visible and cannot be hidden.  On the
  Board layout, enabled fields are displayed below the title on each task card
  in the order defined in the picker.  On the Table (List) layout, each
  enabled field renders as a sortable column, also in picker order.  The
  default visible fields (when a view has no field config yet) are:
  Assignee, Importance, Story Points, and Type — Title is not a picker row
  at all, it is always rendered.  Custom fields of any type (text, number,
  date, select, multi_select, boolean, url) can be toggled on or off
  alongside built-in fields.

  Field settings are per view AND per user: a member without "views.write"
  saves them only for themselves ("Save"), while a "views.write" holder
  chooses between "Save for everyone" (the team default) and "Save only for
  me" (a personal override marked "Only visible to you").  Every scenario
  below says "the user saves the view settings", which is "Save for
  everyone" for a views.write holder (the Background user) — see
  view-settings.feature for the personal-vs-shared behaviour itself.

  ═══════════════════════════════════════════════════════════════════════════
  Background
  ═══════════════════════════════════════════════════════════════════════════

  @authenticated
  Background:
    Given the user already has a stored authenticated session
    And a project named "E2E_FIELDS_PROJECT" exists
    And the project has a "Product Backlog" interaction with at least one Board view and one Table view
    And the user has the "views.write" project permission in "E2E_FIELDS_PROJECT"
    And the user has navigated to the "Product Backlog" interaction inside "E2E_FIELDS_PROJECT"

  ═══════════════════════════════════════════════════════════════════════════
  Rule: Field picker — content and interaction
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: Opening the field picker lists all built-in fields
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    Then a field picker should appear
    And the picker should list the following built-in fields:
      | Assignee     |
      | Status       |
      | Importance   |
      | Story Points |
      | Type         |
      | Epic         |
      | Tags         |
      | Reporter     |
      | Start Date   |
      | Due Date     |
      | Created      |
    And the picker should not list "Title" as a field (it is always visible)

  Scenario: Field picker also lists all project custom fields
    Given the project has a custom field "Story Points" of type "number"
    And the project has a custom field "Severity" of type "select" with options "Low,Medium,High"
    And the project has a custom field "Is Blocked" of type "boolean"
    And the project has a custom field "Target Date" of type "date"
    And the project has a custom field "Notes" of type "text"
    And the project has a custom field "Reference URL" of type "url"
    And the project has a custom field "Labels" of type "multi_select" with options "frontend,backend,devops"
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    Then the field picker should include: "Story Points", "Severity", "Is Blocked", "Target Date", "Notes", "Reference URL", and "Labels"

  Scenario: Enabled fields are shown in a draggable checked section; disabled in an unchecked greyed section
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    Then the field picker should display checked (enabled) fields in the top section
    And the checked fields should each have a drag handle for reordering
    And the field picker should display unchecked (disabled) fields in the bottom section
    And the unchecked fields should appear visually greyed out

  Scenario: The default visible fields are Assignee, Importance, Story Points, and Type
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    Then the following fields should be checked by default:
      | Assignee     |
      | Importance   |
      | Story Points |
      | Type         |
    And the following built-in fields should be unchecked by default:
      | Status       |
      | Epic         |
      | Tags         |
      | Reporter     |
      | Start Date   |
      | Due Date     |
      | Created      |

  Scenario: Checking a disabled field moves it to the enabled section
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user checks the "Due Date" field
    Then "Due Date" should appear in the enabled (checked) section of the picker
    And "Due Date" should no longer appear in the disabled section

  Scenario: Unchecking an enabled field moves it to the disabled section
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user unchecks the "Importance" field
    Then "Importance" should appear in the disabled (unchecked) section of the picker
    And "Importance" should no longer appear in the enabled section

  Scenario: The "Fields" summary row reflects the current enabled field names
    Given the view has visible fields configured as "Assignee, Status"
    When the user clicks the "View settings" button in the view toolbar
    Then the "Fields" row should display the text "Title, Assignee, Status"
    And the "Fields" row should always start with "Title"

  Scenario: The "Fields" summary row shows the defaults when no fields are configured
    When the user clicks the "View settings" button in the view toolbar
    Then the "Fields" row should display the text "Title, Assignee, Importance, Story Points, Type"

  ═══════════════════════════════════════════════════════════════════════════
  Rule: Title is always visible and cannot be hidden
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: Title is not a toggleable row in the field picker
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    Then the field picker should not contain a "Title" checkbox
    And the user should have no way to hide the title

  Scenario: Title is always the primary text on a Board card regardless of field settings
    Given the interaction has a task "E2E_TITLE_TASK"
    And the view has only "Title" and "Assignee" as visible fields
    When the user is on the Board view
    Then every task card should display the task title as the primary (top) text
    And the title should appear above any other enabled fields on the card

  Scenario: Title is always the first column in the List view regardless of field settings
    Given the view has visible fields configured as "Assignee, Status"
    When the user is on the Table view
    Then the "Title" column should always appear first in the row
    And the column order after title should follow the saved field order

  ═══════════════════════════════════════════════════════════════════════════
  Rule: Board view — enabled fields display below the task title on the card
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: A Board card shows Assignee and Type below the title by default
    Given the interaction has a task "E2E_BOARD_DEFAULT" with type "Story" assigned to "E2E_USER"
    When the user is on the Board view
    Then the card for "E2E_BOARD_DEFAULT" should display the task title as the top line
    And below the title the card should display the task type badge for "Story"
    And below the title the card should display the assignee avatar for "E2E_USER"

  Scenario: Enabling "Status" on a Board view shows the status badge below the title
    Given the interaction has a task "E2E_BOARD_STATUS" with status "In Progress"
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user checks the "Status" field
    And the user saves the view settings
    Then the card for "E2E_BOARD_STATUS" should display a status badge showing "In Progress" below the title

  Scenario: Enabling "Importance" on a Board view shows the importance indicator below the title
    Given the interaction has a task "E2E_BOARD_IMPORTANCE" with importance "High"
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user checks the "Importance" field
    And the user saves the view settings
    Then the card for "E2E_BOARD_IMPORTANCE" should display an importance indicator showing "High" below the title

  Scenario: Disabling "Assignee" removes the assignee avatar from Board cards
    Given the interaction has a task "E2E_BOARD_NO_ASSIGNEE" assigned to "E2E_USER"
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user unchecks the "Assignee" field
    And the user saves the view settings
    Then the card for "E2E_BOARD_NO_ASSIGNEE" should not display an assignee avatar

  Scenario: Disabling "Type" removes the task type badge from Board cards
    Given the interaction has a task "E2E_BOARD_NO_TYPE" with type "Bug"
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user unchecks the "Type" field
    And the user saves the view settings
    Then the card for "E2E_BOARD_NO_TYPE" should not display a task type badge

  Scenario: Enabling a custom "select" field shows the selected option on the Board card
    Given the project has a custom field "Severity" of type "select" with options "Low,Medium,High"
    And the interaction has a task "E2E_BOARD_SEV" with custom field "Severity" set to "High"
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user checks the "Severity" field
    And the user saves the view settings
    Then the card for "E2E_BOARD_SEV" should display "High" for the "Severity" field below the title

  Scenario: Enabling a custom "number" field shows the numeric value on the Board card
    Given the project has a custom field "Story Points" of type "number"
    And the interaction has a task "E2E_BOARD_SP" with custom field "Story Points" set to 8
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user checks the "Story Points" field
    And the user saves the view settings
    Then the card for "E2E_BOARD_SP" should display "8" for the "Story Points" field below the title

  Scenario: Enabling a custom "boolean" field shows a checked or unchecked indicator on the Board card
    Given the project has a custom field "Is Blocked" of type "boolean"
    And the interaction has a task "E2E_BOARD_BLOCKED" with custom field "Is Blocked" set to true
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user checks the "Is Blocked" field
    And the user saves the view settings
    Then the card for "E2E_BOARD_BLOCKED" should display a checked indicator for "Is Blocked" below the title

  Scenario: Enabling a custom "date" field shows a formatted date on the Board card
    Given the project has a custom field "Target Date" of type "date"
    And the interaction has a task "E2E_BOARD_DATE" with custom field "Target Date" set to "2026-06-30"
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user checks the "Target Date" field
    And the user saves the view settings
    Then the card for "E2E_BOARD_DATE" should display a formatted date for "Target Date" below the title

  Scenario: Enabling a custom "text" field shows a truncated text value on the Board card
    Given the project has a custom field "Notes" of type "text"
    And the interaction has a task "E2E_BOARD_TEXT" with custom field "Notes" set to "A very long description that should be truncated on the card"
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user checks the "Notes" field
    And the user saves the view settings
    Then the card for "E2E_BOARD_TEXT" should display a truncated text value for "Notes" below the title

  Scenario: Tasks with no value for an enabled custom field show an empty placeholder on the Board card
    Given the project has a custom field "Severity" of type "select" with options "Low,Medium,High"
    And the interaction has a task "E2E_BOARD_NO_SEV" without the "Severity" custom field set
    And the view has "Severity" enabled in the field picker
    When the user is on the Board view
    Then the card for "E2E_BOARD_NO_SEV" should show an empty or dash placeholder for "Severity"
    And no error should occur

  Scenario: Fields on a Board card appear in the order defined in the field picker
    Given the interaction has a task "E2E_BOARD_ORDER" with type "Story" assigned to "E2E_USER" with importance "Medium"
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user reorders the enabled fields so the order is: Title, Importance, Type, Assignee
    And the user saves the view settings
    Then the card for "E2E_BOARD_ORDER" should display fields below the title in the order: Importance, Type, Assignee

  ═══════════════════════════════════════════════════════════════════════════
  Rule: List (Table) view — enabled fields display as columns
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: Default Table view shows Assignee, Importance, Story Points, and Type columns alongside Title
    Given the interaction has a task "E2E_LIST_DEFAULT"
    When the user is on the Table view
    Then the table should contain a "Title" column
    And the table should contain a "Type" column
    And the table should contain an "Importance" column
    And the table should contain a "Story Points" column
    And the table should contain an "Assignee" column

  Scenario: Toggling a field off removes its column from the Table view
    Given the user is on the Table view
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user unchecks the "Importance" field
    And the user saves the view settings
    Then the table should not display an "Importance" column header
    And task rows should not show the importance value

  Scenario: Toggling a field on adds its column to the Table view
    Given the user is on the Table view
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user checks the "Due Date" field
    And the user saves the view settings
    Then the table should display a "Due Date" column header
    And each task row should display the task's due date in that column

  Scenario: Toggling a custom "select" field on adds it as a column in the Table view
    Given the project has a custom field "Severity" of type "select" with options "Low,Medium,High"
    And the interaction has a task "E2E_LIST_SEV" with custom field "Severity" set to "Medium"
    And the user is on the Table view
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user checks the "Severity" field
    And the user saves the view settings
    Then the table should display a "Severity" column header
    And the row for "E2E_LIST_SEV" should display "Medium" in the "Severity" column

  Scenario: Toggling a custom "number" field on adds it as a column in the Table view
    Given the project has a custom field "Story Points" of type "number"
    And the interaction has a task "E2E_LIST_SP" with custom field "Story Points" set to 13
    And the user is on the Table view
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user checks the "Story Points" field
    And the user saves the view settings
    Then the table should display a "Story Points" column header
    And the row for "E2E_LIST_SP" should display "13" in the "Story Points" column

  Scenario: Toggling a custom "boolean" field on adds it as a column in the Table view
    Given the project has a custom field "Is Blocked" of type "boolean"
    And the interaction has a task "E2E_LIST_BLOCKED" with custom field "Is Blocked" set to true
    And the user is on the Table view
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user checks the "Is Blocked" field
    And the user saves the view settings
    Then the table should display an "Is Blocked" column header
    And the row for "E2E_LIST_BLOCKED" should display a checked indicator in the "Is Blocked" column

  Scenario: Toggling a custom "multi_select" field on adds it as a column in the Table view
    Given the project has a custom field "Labels" of type "multi_select" with options "frontend,backend,devops"
    And the interaction has a task "E2E_LIST_LABELS" with custom field "Labels" set to ["frontend", "backend"]
    And the user is on the Table view
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user checks the "Labels" field
    And the user saves the view settings
    Then the table should display a "Labels" column header
    And the row for "E2E_LIST_LABELS" should display both "frontend" and "backend" tags in the "Labels" column

  Scenario: Tasks with no value for an enabled custom field show an empty cell in the Table view
    Given the project has a custom field "Severity" of type "select" with options "Low,Medium,High"
    And the interaction has a task "E2E_LIST_NO_SEV" without the "Severity" custom field set
    And the view has "Severity" enabled in the field picker
    When the user is on the Table view
    Then the row for "E2E_LIST_NO_SEV" should display an empty or dash placeholder in the "Severity" column

  Scenario: Column order in the Table view matches the enabled field order in the picker
    Given the user is on the Table view
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user reorders the enabled fields so the order is: Status, Assignee, Importance, Type
    And the user saves the view settings
    Then the table columns should appear in the order: Title, Status, Assignee, Importance, Type

  ═══════════════════════════════════════════════════════════════════════════
  Rule: Field settings are saved independently per view
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: Hiding a field on the Board view does not affect the Table view's columns
    Given the user has a "Board" view and a "Table" view in the interaction
    When the user is on the "Board" view
    And the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user unchecks the "Importance" field
    And the user saves the view settings
    And the user switches to the "Table" view
    Then the table view should still display an "Importance" column

  Scenario: Enabling a custom field on the Table view does not affect the Board view's cards
    Given the project has a custom field "Severity" of type "select" with options "Low,Medium,High"
    And the user has a "Board" view and a "Table" view in the interaction
    When the user is on the "Table" view
    And the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    And the user checks the "Severity" field
    And the user saves the view settings
    And the user switches to the "Board" view
    Then Board cards should not display the "Severity" value

  Scenario: Field settings survive a page reload
    Given the user has enabled "Due Date" and disabled "Importance" on the active Table view
    When the user reloads the page and navigates back to the interaction
    Then the table view should display the "Due Date" column
    And the table view should not display the "Importance" column

  ═══════════════════════════════════════════════════════════════════════════
  Rule: Newly added or deleted custom fields are reflected in the field picker
  ═══════════════════════════════════════════════════════════════════════════

  Scenario: Creating a new custom field makes it available in the field picker
    Given the project initially has no custom fields
    And the user creates a custom field "Priority Score" of type "number"
    When the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    Then the field picker should list "Priority Score" in the disabled section

  Scenario: Deleting a custom field removes it from the field picker and from the view
    Given the project has a custom field "Obsolete Field" of type "text"
    And the view has "Obsolete Field" enabled
    When the user deletes the "Obsolete Field" custom field
    And the user clicks the "View settings" button in the view toolbar
    And the user clicks the "Fields" row
    Then the field picker should no longer list "Obsolete Field"
    And the Table view should no longer display an "Obsolete Field" column
    And Board cards should no longer show a value for "Obsolete Field"
