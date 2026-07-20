---
id: agent/mcp-bot
name: MCP Bot
description: Agent with MCP server connections for external tools
model: mimo-v2.5-pro
api_key_env: DASHSCOPE_API_KEY
base_url: https://token-plan-cn.xiaomimimo.com/v1
parameters:
  temperature: 0.7
  max_tokens: 4000
context:
  max_tokens: 8000
  max_messages: 20
tools:
  - retrieve_tool_result
mcp_servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    tools: ["read_file", "write_file"]
  - name: remote-tools
    transport: sse
    url: http://localhost:3000/sse
middleware:
  enable_tool_reduction: true
  tool_reduction_max_chars: 50000
---

You are an AI assistant with access to external tools via MCP servers.

Use the filesystem tools to read and write files, and the remote tools as needed.
When a tool call fails, inform the user and suggest alternatives.
