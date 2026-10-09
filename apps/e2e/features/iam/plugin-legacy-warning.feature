@iam @plugins
Feature: Plugins on the old permission model
  A plugin whose package still uses the retired requirePermissions middleware is
  flagged legacy_permissions by the plugin list. Administrators with
  plugins:write see a dismissible banner on the home page, and the marketplace
  card of that plugin carries an "Uses old permissions" badge.

  No real legacy plugin can be installed, so the installed-plugin list and the
  marketplace catalogue are stubbed with page.route before signing in.

  Rule: The home page banner

    Scenario: An administrator sees the banner naming the plugin, with a link to manage plugins
      Given the installed plugins are "Legacy Reports" (legacy) and "Modern Time Log"
      When the administrator signs in
      Then a banner "Plugin needs an update" names "Legacy Reports" and not "Modern Time Log"
      And it mentions requirePermissions and requireActions
      When "Manage plugins" is clicked
      Then the plugins page opens

    Scenario: Several affected plugins are named together under a plural title
      Given two installed plugins are legacy
      Then the banner reads "Plugins need an update" and names both

    Scenario: No banner while every plugin uses the new permissions
      Given the only installed plugin is not legacy
      Then there is no banner

    Scenario: Dismissing hides it, remembers the choice, and a newly affected plugin shows it again
      When the banner is dismissed
      Then it disappears and localStorage "paca-legacy-plugins-banner-dismissed" holds the plugin name
      And it stays hidden after a reload
      When a second plugin becomes affected
      Then the banner shows again as "Plugins need an update"

    Scenario: Someone without plugins:write never sees it
      Given a user holding projects:read and plugins:read but not plugins:write
      Then the home page shows no banner and no mention of the plugin

    Scenario: A user holding plugins:write does see it
      Given a user holding plugins:write
      Then the banner names the legacy plugin

  Rule: The marketplace card

    Background:
      Given the user already has a stored authenticated admin session
      And the user opens the Marketplace tab of the plugins page

    Scenario: An installed legacy plugin carries an 'Uses old permissions' badge and an explanation
      Then its card shows "Installed", the badge "Uses old permissions" and an alert saying it can't be reinstalled or upgraded until its author publishes a version that uses requireActions

    Scenario: A plugin on the new permissions has no badge
      Then its card shows "Installed" and neither the badge nor the alert

    Scenario: A legacy plugin that is not installed shows no badge
      Given the catalogue lists it but nothing is installed
      Then its card has no "Uses old permissions" badge
