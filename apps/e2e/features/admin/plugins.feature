@admin @plugins
Feature: Plugin settings
  The Plugins page (Admin > Plugins, route /admin/plugins) is titled "Plugin
  Settings" and has two tabs: "Marketplace" (shown by default) and
  "Extension Point Layout". The Marketplace tab lists plugins from the public
  paca-plugins catalog (GET /api/v1/admin/plugins/marketplace) as cards with
  name, version, description, feature badges and an Install / Uninstall
  action, and has a "Search plugins" box that filters by name, display name
  or description. The Extension Point Layout tab lets an administrator
  reorder and hide plugin panels; when no installed plugin contributes an
  extension point it shows an empty state. The page is gated by the global
  "plugins.write" permission: users without it are redirected to the home
  page. A plugin can also contribute its own full admin page at
  /admin/plugins/{pluginId}/{slug}; an unknown plugin/slug combination
  renders the router's Not Found page. The public marketplace catalog and the
  installed-plugin list are stubbed in these scenarios, and installing,
  uninstalling and upgrading plugins are deliberately not exercised because
  they need a real, downloadable plugin package.

  @authenticated
  Rule: Access is gated by the plugins.write permission

    Background:
      Given the user already has a stored authenticated session

    Scenario: A user without plugins.write is redirected away from the Plugins page
      Given a user exists whose global role does not grant "plugins.write"
      When that user signs in and navigates to the Plugins page
      Then the user should be redirected to the Home page
      And the "Plugin Settings" heading should not be displayed

    Scenario: A user with only plugins.write can open the Plugins page
      Given a user exists whose global role grants "plugins.write"
      When that user signs in and navigates to the Plugins page
      Then the "Plugin Settings" heading should be displayed
      And the "Marketplace" and "Extension Point Layout" tabs should be displayed

  @authenticated
  Rule: Marketplace tab

    Background:
      Given the user already has a stored authenticated session
      And the marketplace catalog contains the plugins "Time Logging" and "Release Notes"
      And no plugins are installed
      And the user has navigated to the Plugins page

    Scenario: The Marketplace tab is shown by default
      Then the "Plugin Settings" heading should be displayed
      And the Marketplace card description should be displayed
      And the marketplace should list "Time Logging" and "Release Notes"

    Scenario: A marketplace card shows its version, description and feature badges
      Then the "Time Logging" card should display its version "1.2.0"
      And the "Time Logging" card should display its description
      And the "Time Logging" card should display the "Backend" and "Frontend" feature badges
      And the "Time Logging" card should offer an "Install" button

    Scenario: Searching the marketplace filters the plugin list
      When the user types "release" into the "Search plugins" box
      Then the marketplace should list "Release Notes"
      And the marketplace should not list "Time Logging"

    Scenario: Searching for a plugin that does not exist shows an empty state
      When the user types "no-such-plugin" into the "Search plugins" box
      Then the marketplace should display "No marketplace plugins found."

    Scenario: An empty marketplace catalog shows an empty state
      Given the marketplace catalog is empty
      When the user reloads the Plugins page
      Then the marketplace should display "No marketplace plugins found."

  @authenticated
  Rule: Installed plugins are reflected on marketplace cards

    Background:
      Given the user already has a stored authenticated session
      And the marketplace catalog contains the plugin "Time Logging" at version "1.2.0"

    Scenario: An installed plugin shows the Installed badge and an Uninstall button
      Given the plugin "Time Logging" is installed at version "1.2.0"
      When the user navigates to the Plugins page
      Then the "Time Logging" card should display the "Installed" badge
      And the "Time Logging" card should offer an "Uninstall" button
      And the "Time Logging" card should not offer an "Install" button
      And the "Time Logging" card should not display the "Update available" badge

    Scenario: An installed plugin with a newer marketplace version offers an upgrade
      Given the plugin "Time Logging" is installed at version "1.0.0"
      When the user navigates to the Plugins page
      Then the "Time Logging" card should display the "Update available" badge
      And the "Time Logging" card should offer an "Upgrade to 1.2.0" button

  @authenticated
  Rule: Extension Point Layout tab

    Background:
      Given the user already has a stored authenticated session
      And no plugins are installed
      And the user has navigated to the Plugins page

    Scenario: The Layout tab shows an empty state when no plugin contributes extension points
      When the user clicks the "Extension Point Layout" tab
      Then the Extension Point Layout card description should be displayed
      And the layout panel should display "No plugins with extension points are installed."

    Scenario: Switching back to the Marketplace tab shows the catalog again
      When the user clicks the "Extension Point Layout" tab
      And the user clicks the "Marketplace" tab
      Then the "Search plugins" box should be displayed

  @authenticated
  Rule: Plugin-contributed admin pages

    Background:
      Given the user already has a stored authenticated session

    # @fixme: the app currently shows its generic "Something went wrong" error state
    # here instead of a Not Found page (notFound() thrown from the route component
    # has no notFoundComponent). The spec is test.fixme until that is fixed.
    @fixme
    Scenario: Navigating to an admin page of a plugin that is not installed shows Not Found
      When the user navigates to "/admin/plugins/no-such-plugin/no-such-page"
      Then the "Not Found" page should be displayed
      And the "Plugin Settings" heading should not be displayed
