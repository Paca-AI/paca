@admin @changelog
Feature: Changelog
  The Changelog page (Admin > Changelog, route /admin/changelog) is titled
  "What's New" and lists the published Paca releases fetched from
  GET /api/v1/releases (a backend-cached mirror of the GitHub releases feed).
  Each release is rendered as a card with its name, publish date, a link to
  the release on GitHub, and its release notes — a small, safe subset of
  markdown (headings, bullet lists, bold, inline code, and http(s)/mailto
  links). The release matching the running version gets a "Current version"
  badge. The page has three data states: releases available, no releases
  ("No release notes available yet."), and a failed request ("Couldn't load
  the changelog right now. Please try again later."). There is no real way to
  seed GitHub release data, so these scenarios stub the /releases response.

  @authenticated
  Rule: Release list

    Background:
      Given the user already has a stored authenticated session
      And the /releases endpoint returns the releases "v1.2.0" (current) and "v1.1.0"

    Scenario: The changelog page shows its title and description
      When the user navigates to the Changelog page
      Then the page should display the heading "What's New"
      And the page should display "Follow the improvements and highlights from the latest Paca releases."

    Scenario: Releases are listed in the order returned by the API
      When the user navigates to the Changelog page
      Then the release cards should be listed as "Paca 1.2.0" followed by "Paca 1.1.0"

    Scenario: Only the running release carries the "Current version" badge
      When the user navigates to the Changelog page
      Then the "Paca 1.2.0" card should display a "Current version" badge
      And the "Paca 1.1.0" card should not display a "Current version" badge
      And exactly one "Current version" badge should be visible on the page

    Scenario: A release card shows its publish date and a link to the release
      When the user navigates to the Changelog page
      Then the "Paca 1.2.0" card should display the publish date "Mar 4, 2026"
      And the "Paca 1.2.0" card should contain a link labelled "v1.2.0" pointing at the release URL

    Scenario: Release notes render headings, bullet lists, bold, inline code and links
      When the user navigates to the Changelog page
      Then the "Paca 1.2.0" card should show a "Features" sub-heading
      And the card should list the bullets "Added dark mode" and "Fixed login bug"
      And "dark mode" should be rendered in bold
      And "login" should be rendered as inline code
      And the card should contain a link "docs" pointing at "https://example.com/docs"

    Scenario: Links with an unsafe protocol are rendered as plain text
      Given a release note contains the markdown link "[click me](javascript:alert(1))"
      When the user navigates to the Changelog page
      Then "click me" should be displayed as plain text
      And no link named "click me" should exist on the page

    Scenario: A release without notes shows a placeholder
      Given the release "v1.0.0" has an empty body
      When the user navigates to the Changelog page
      Then the "Paca 1.0.0" card should display "—" in place of release notes

  @authenticated
  Rule: Empty and error states

    Background:
      Given the user already has a stored authenticated session

    Scenario: No published releases shows an empty state
      Given the /releases endpoint returns no releases
      When the user navigates to the Changelog page
      Then the page should display "No release notes available yet."
      And no release cards should be displayed

    Scenario: A failing releases request shows an error state
      Given the /releases endpoint responds with a server error
      When the user navigates to the Changelog page
      Then the page should display "Couldn't load the changelog right now. Please try again later."
      And no release cards should be displayed
