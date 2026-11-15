@docs
Feature: Document ETag through the gateway
  The gateway compresses responses and may append the content coding to a
  document's ETag. A client that sends back the ETag it received, unchanged,
  in If-Match on a content write gets the write through.

  @authenticated
  Rule: If-Match with the gateway's ETag

    Background:
      Given the user already has a stored authenticated session
      And a project exists

    Scenario: The ETag the gateway returns works as If-Match on a content write
      Given a document with enough content to be compressed
      When the client reads the document with "Accept-Encoding: gzip"
      And sends a content update with the returned ETag in If-Match
      Then the update succeeds and the content hash changes
