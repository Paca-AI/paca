@ui @sidebar
Feature: Sidebar navigation
  The application sidebar should provide intuitive navigation, persist its
  collapsed / expanded state across page loads, and adapt correctly to
  both desktop and mobile viewports.  Admin-only sections must follow the
  user's permission profile.

  @authenticated
  Rule: Expanding and collapsing the sidebar

    Background:
      Given the user already has a stored authenticated session
      And the user is on the home page

    Scenario: Clicking the trigger button collapses the sidebar
      Given the sidebar is expanded
      When the user clicks the sidebar trigger button
      Then the sidebar should be in collapsed state
      And the sidebar should shrink to an icon rail

    Scenario: Clicking the trigger button again expands the sidebar
      Given the sidebar is collapsed
      When the user clicks the sidebar trigger button
      Then the sidebar should be in expanded state
      And each navigation item should show its label alongside the icon

    Scenario: Keyboard shortcut toggles the sidebar
      Given the sidebar is expanded
      When the user presses the "Ctrl+B" (or "Cmd+B") keyboard shortcut
      Then the sidebar should be in collapsed state
      When the user presses the same keyboard shortcut again
      Then the sidebar should be in expanded state

    Scenario: Clicking the sidebar rail collapses and expands the sidebar
      Given the sidebar is expanded
      When the user clicks the sidebar rail
      Then the sidebar should be in collapsed state
      When the user clicks the sidebar rail again
      Then the sidebar should be in expanded state

    Scenario: Collapsed navigation items show tooltips on hover
      Given the sidebar is collapsed
      When the user hovers over the "Home" navigation icon
      Then a tooltip with the label "Home" should be visible

  @authenticated
  Rule: Navigation and active states

    Background:
      Given the user already has a stored authenticated session

    Scenario: Active page is highlighted in the sidebar
      When the user is on the home page
      Then the "Home" navigation item should be marked as active
      And the active item should have a distinct visual indicator

    Scenario: Navigating to a page updates the active highlight
      Given the user is on the home page
      When the user clicks the "Global Roles" navigation item
      Then the "Global Roles" navigation item should be marked as active
      And the "Home" navigation item should no longer be marked as active
      And the browser should be on the global roles page

    Scenario: Navigating to the Users page updates the active highlight
      Given the user is on the home page
      When the user clicks the "Users" navigation item
      Then the "Users" navigation item should be marked as active
      And the "Home" navigation item should no longer be marked as active
      And the browser should be on the users page

  @authenticated
  Rule: Admin section visibility

    Scenario: Administration section is visible to admin users
      Given the user already has a stored authenticated admin session
      When the user views the sidebar
      Then the "Administration" section label should be visible
      And the "Global Roles" navigation item should be visible
      And the "Users" navigation item should be visible

    Scenario: Administration section is hidden for users without any admin permission
      Given a user whose global role grants no administration permission
      And the user is signed in
      When the user views the sidebar
      Then the "Home" navigation item should be visible
      And the "Administration" section label should not be visible
      And the "Global Roles" navigation item should not be visible
      And the "Users" navigation item should not be visible
      And the "What's New" navigation item should not be visible

    Scenario: A user with a single admin permission only sees the matching item
      Given a user whose global role only grants "users.read"
      And the user is signed in
      When the user views the sidebar
      Then the "Administration" section label should be visible
      And the "Users" navigation item should be visible
      And the "Global Roles" navigation item should not be visible
      And the "Plugins" navigation item should not be visible
      And the "Settings" navigation item should not be visible

    Scenario: Admin items show tooltips when sidebar is collapsed
      Given the user already has a stored authenticated admin session
      And the sidebar is collapsed
      When the user hovers over the "Global Roles" icon
      Then a tooltip with the label "Global Roles" should be visible

  @authenticated
  Rule: Theme switcher interaction

    Background:
      Given the user already has a stored authenticated session
      And the user is on the home page

    Scenario: Theme switcher shows three options when sidebar is expanded
      Given the sidebar is expanded
      Then the theme switcher should show the "Light" option
      And the theme switcher should show the "Dark" option
      And the theme switcher should show the "Auto" option
      And the currently active theme option should be highlighted

    Scenario: Selecting a theme changes the application appearance
      Given the sidebar is expanded
      When the user selects the "Dark" theme option
      Then the page should switch to dark theme
      And the "Dark" option should be highlighted in the theme switcher
      And the "Light" option should no longer be highlighted

    Scenario: Theme switcher shows a single cycling button when collapsed
      Given the sidebar is collapsed
      Then the three theme options should be hidden
      And a single theme cycling button should be visible in the footer

    Scenario: Cycling button advances the theme on each click
      Given the theme is "Light" and the sidebar is collapsed
      When the user clicks the cycling theme button
      Then the theme should become "Dark" and the icon should change to a moon
      When the user clicks the cycling theme button again
      Then the theme should become "Auto" and the icon should change to a monitor

  @authenticated
  Rule: State persistence

    Background:
      Given the user already has a stored authenticated session
      And the user is on the home page on a desktop viewport

    Scenario: State is stored in a browser cookie
      When the user collapses the sidebar
      Then a "sidebar_state" cookie with the value "false" should be present in the browser
      When the user expands the sidebar
      Then the "sidebar_state" cookie should have the value "true"

    # Known bug: the cookie is written on every toggle but never read back, so
    # the sidebar always starts expanded.  Implemented as test.fixme.
    @fixme
    Scenario: Collapsed state persists after a page reload
      Given the sidebar is expanded
      When the user clicks the sidebar trigger button
      And the user reloads the page
      Then the sidebar should still be in collapsed state

    Scenario: A sidebar that was left expanded is still expanded after a page reload
      Given the sidebar is expanded
      When the user reloads the page
      Then the sidebar should still be in expanded state

  @authenticated
  Rule: User menu

    Background:
      Given the user already has a stored authenticated admin session
      And the user is on the home page

    Scenario: The sidebar footer shows the user's profile and a Log out action
      Then the user profile button showing the user's name and role should be visible in the sidebar footer
      When the user clicks the user profile button
      Then a "Log out" menu item should be visible

  @mobile
  Rule: Mobile responsive behaviour

    Background:
      Given the user already has a stored authenticated session
      And the viewport is set to a mobile size

    Scenario: Sidebar opens as an overlay sheet on mobile
      When the user clicks the sidebar trigger button
      Then the sidebar should slide in as an overlay sheet
      And the main content should remain visible behind the overlay
      And the page content should not be pushed aside

    Scenario: Mobile sidebar can be dismissed with the Escape key
      Given the mobile sidebar is open
      When the user presses the Escape key
      Then the sidebar sheet should close

    Scenario: Brand logo and app name are visible inside the mobile sheet
      Given the mobile sidebar is open
      Then the "paca" brand name should be visible
      And the paca logo should be visible
