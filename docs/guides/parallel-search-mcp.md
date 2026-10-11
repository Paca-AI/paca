# Web research with Parallel Search MCP

Give a Paca-managed **LLM agent** free web search and page fetching with
[Parallel Search MCP](https://docs.parallel.ai/integrations/mcp/search-mcp).
The anonymous endpoint needs no Parallel account or API key. Search uses Fast
mode, with free-tier rate limits intended for exploration and light use.

## Add it to an agent

Start with a working Paca installation and an `llm` agent with its model provider
configured. This setup uses the existing agent-runner and Goose HTTP MCP support;
it needs no extra package in the agent's sandbox.

1. Open the agent's **MCP Servers** tab and choose **Add MCP Server**.
2. Set **Server Name** to `parallel-search`, **Transport** to **HTTP**, and **URL**
   to `https://search.parallel.ai/mcp`.
3. Add the server and leave it enabled. Start a new conversation so Goose loads
   the updated MCP configuration.

For API-managed configuration, use the existing
[agent MCP server endpoint](../ai-agent/api-design.md#agent-mcp-servers) with this
request body:

```json
{
  "server_name": "parallel-search",
  "transport": "http",
  "url": "https://search.parallel.ai/mcp"
}
```

This is opt-in per agent. It adds tools alongside the built-in Paca MCP server
and leaves the agent's model and existing MCP servers in place. Local `acp`
agents configure MCP servers in their own CLI; this guide covers `llm` agents.

## Use it in a conversation

Try: **"Use Parallel to search for Paca's public documentation on agent execution
models. Summarize the differences and cite the source URLs."** The agent can use
`web_search` with an `objective` and one or more `search_queries`.

For a specific page, try: **"Use Parallel to fetch
https://paca-ai.org and summarize the installation options."** The `web_fetch`
tool accepts `urls` and an optional `objective`. Excerpts are the default;
requesting full content can produce much larger results.

Research queries and requested page URLs are sent to Parallel. Avoid including
private project data in research prompts. Use the anonymous `/mcp` URL above;
`/mcp-oauth` requires authentication. The sandbox needs outbound HTTPS access to
`search.parallel.ai`. If the service reports a rate limit, retry after the
indicated delay rather than adding repeated tool calls to the conversation.
