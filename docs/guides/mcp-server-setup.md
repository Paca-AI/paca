# MCP Server Setup Guide

This guide walks you through setting up the Paca MCP (Model Context Protocol) server to integrate with your AI agents, enabling them to interact with Paca projects, tasks, sprints, and documents.

## Prerequisites

Before setting up the MCP server, ensure you have:

- A running Paca API instance (local or deployed)
- An API key from your Paca instance (generate it in user settings)
- Node.js 18+ installed (required by MCP clients)

## Quick Start

The Paca MCP server is available as a GitHub package — no installation or build step required. Simply configure your MCP client to pull and run it directly using `npx`.

### Package Information

- **Package**: `@paca-ai/paca-mcp`
- **Repository**: [github.com/paca-ai/paca](https://github.com/paca-ai/paca) (MCP server source lives under `apps/mcp`)

### Checking the Package

To inspect the latest version of the MCP package:

```bash
npx @paca-ai/paca-mcp --version
```

## Agent-Specific Setup

The MCP server can be integrated with various AI agents and platforms. Below are configuration guides for popular options.

### Claude Desktop (Recommended)

Claude Desktop provides the most seamless integration with the Paca MCP server.

**Configuration Steps:**

1. Locate your Claude Desktop config file:
   - **macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
   - **Windows**: `%APPDATA%\Claude\claude_desktop_config.json`

2. Add the following configuration:

```json
{
  "mcpServers": {
    "paca": {
      "command": "npx",
      "args": [
        "-y",
        "@paca-ai/paca-mcp"
      ],
      "env": {
        "PACA_API_KEY": "your-api-key-here",
        "PACA_API_URL": "http://localhost:8080"
      }
    }
  }
}
```

3. Replace the placeholder values:
   - `your-api-key-here` with your actual Paca API key
   - `http://localhost:8080` with your Paca API URL if different

4. Restart Claude Desktop

**Note**: The `npx -y @paca-ai/paca-mcp` command automatically downloads and runs the latest version of the Paca MCP server from npm.

**Usage in Claude Desktop:**

Once configured, Claude will automatically have access to all 81 Paca tools. You can ask Claude to:

- "List all projects in my Paca workspace"
- "Create a new task for user authentication"
- "Create a sprint for the next 2 weeks"
- "Update the task status to in progress"
- "Add a comment to the design document"

### Other MCP-Compatible Clients

The Paca MCP server follows the standard MCP protocol and can be used with any MCP-compatible client.

**Required Configuration:**

1. **Command**: Use `npx -y @paca-ai/paca-mcp` to automatically download and run the latest version
2. **Environment Variables**:
   - `PACA_API_KEY` (required): Your Paca API key
   - `PACA_API_URL` (optional): Paca API URL (default: `http://localhost:8080`)

**Example Client Configuration:**

Most MCP clients will accept configuration in this format:

```json
{
  "name": "paca",
  "command": "npx",
  "args": [
    "-y",
    "@paca-ai/paca-mcp"
  ],
  "env": {
    "PACA_API_KEY": "your-api-key-here",
    "PACA_API_URL": "http://localhost:8080"
  }
}
```

### Custom AI Agents

For custom AI agents or applications, you can use the MCP server programmatically:

```javascript
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";

const transport = new StdioClientTransport({
  command: "npx",
  args: ["-y", "@paca-ai/paca-mcp"],
  env: {
    PACA_API_KEY: "your-api-key-here",
    PACA_API_URL: "http://localhost:8080"
  }
});

const client = new Client({
  name: "my-agent",
  version: "1.0.0"
}, {
  capabilities: {}
});

await client.connect(transport);

// List available tools
const tools = await client.listTools();
console.log("Available tools:", tools.tools);

// Call a tool
const result = await client.callTool({
  name: "list_projects",
  arguments: {}
});
console.log("Projects:", result.content);
```

## Available Tools

The Paca MCP server provides **81 tools** across **16 categories**:

- 📁 **Project Management** (5 tools): Create, read, update, delete projects
- ✅ **Task Management** (6 tools): Full task lifecycle management
- 🏃 **Sprint Management** (6 tools): Complete sprint workflow
- 📄 **Document Management** (5 tools): Document CRUD operations
- 👥 **Project Members** (5 tools): Team and role assignment (`add_project_member`, `update_project_member_role` take `roleIds`; `get_my_project_permissions` lists the IAM actions you hold)
- 🎭 **Project Roles** (4 tools: `list_project_roles`, `create_project_role`, `update_project_role`, `delete_project_role`): Custom role definitions as IAM policy documents (see [IAM authorization](iam-authorization.md) and [Roles and policies](roles-and-policies.md)). A project role may only name its own project's resources
- 🏷️ **Task Types** (5 tools): Task type configurations
- 📊 **Task Statuses** (4 tools): Workflow status management
- 🎯 **Views** (9 tools): Sprint, backlog, and timeline views
- 🔧 **Custom Fields** (5 tools): Custom field definitions
- 📎 **Attachments** (3 tools): File attachment management
- 📁 **Document Folders** (4 tools): Document organization
- 📸 **Document Snapshots** (2 tools): Document versioning
- 🔗 **GitHub Integration** (7 tools): Repository and PR linking
- 💬 **Task Activities** (4 tools): Comments and activity tracking
- 🔀 **Task GitHub** (5 tools): Branch and PR management

For a complete list of all tools with detailed descriptions, see the [MCP README](../../apps/mcp/README.md).

## Markdown/BlockNote Conversion

The MCP server automatically handles content conversion:

- **Reading**: Fetches content as BlockNote JSON and converts to Markdown for readability
- **Writing**: Accepts Markdown input and converts to BlockNote JSON for storage

This allows your AI agent to work with familiar Markdown format while Paca stores content in rich text format.

## Example Agent Interactions

### Example 1: Create a Complete Sprint Workflow

```
User: "Create a new sprint for next week and add these tasks: 
1. Implement authentication 
2. Set up database 
3. Create user API"

Agent: (uses MCP tools)
1. create_sprint - Creates sprint "Sprint 1" with dates
2. create_task - Creates "Implement authentication" task
3. create_task - Creates "Set up database" task  
4. create_task - Creates "Create user API" task
5. bulk_move_tasks - Moves all tasks to the new sprint
```

### Example 2: Review and Update Task Status

```
User: "Review all in-progress tasks and update their status based on completion"

Agent: (uses MCP tools)
1. list_tasks - Gets all tasks with "in_progress" status
2. get_task - Retrieves details for each task
3. update_task - Updates status to "done" or "blocked" based on analysis
```

### Example 3: Document Management

```
User: "Create a system design document for the authentication module"

Agent: (uses MCP tools)
1. create_document - Creates "Authentication System Design" document
2. update_document - Adds Markdown content with architecture diagrams
3. create_doc_folder - Optionally organizes in "Architecture" folder
```

## Testing the MCP Server

Once configured, you can test the MCP server directly through your MCP client:

### Testing with Claude Desktop

After restarting Claude Desktop, simply ask Claude:
- "What Paca tools are available?"
- "List all my projects"
- "Create a test task"

### Testing with Custom Clients

Use the client's built-in testing tools to:
- List available tools
- Call sample tools
- Verify API authentication

### For Contributors / Advanced Testing

To test with the MCP Inspector, clone the repository and run it locally:

```bash
git clone https://github.com/paca-ai/paca.git
cd paca/apps/mcp
npm install
npm run inspector
```

## Troubleshooting

### Common Issues

**Issue**: "Connection refused" error
- **Solution**: Ensure Paca API is running and `PACA_API_URL` is correct

**Issue**: "Unauthorized" error
- **Solution**: Verify `PACA_API_KEY` is valid and that the user or agent behind it has a role allowing the action (`403` means no attached role allows it; see [IAM authorization](iam-authorization.md#troubleshooting-and-faq))

**Issue**: "npx: command not found" error
- **Solution**: Ensure Node.js 18+ is installed and npx is in your PATH

**Issue**: Claude Desktop doesn't show Paca tools
- **Solution**: Check config file path, verify JSON syntax, and restart Claude Desktop

**Issue**: "Cannot find package '@paca-ai/paca-mcp'" error
- **Solution**: Ensure you have internet connectivity and npm registry access

### Debug Mode

Enable debug logging by setting:

```bash
export DEBUG="*"
```

Then run the MCP server to see detailed logs.

## Security Best Practices

1. **Never commit API keys** to version control
2. **Use environment variables** for sensitive configuration
3. **Limit API key permissions** to only what your agent needs
4. **Rotate API keys** regularly
5. **Use HTTPS** for production deployments

## Next Steps

- Explore the [complete tool documentation](../../apps/mcp/ALL_TOOLS.md)
- Learn about the [MCP server architecture](../../apps/mcp/ARCHITECTURE.md)
- Review [Paca API documentation](../api/README.md) for deeper integration
- Check the [main MCP README](../../apps/mcp/README.md) for development guide

## Getting Help

- Report issues: [GitHub Issues](https://github.com/paca-ai/paca/issues)
- Documentation: [docs/README.md](../README.md)
- Contributing: [CONTRIBUTING.md](../../CONTRIBUTING.md)
