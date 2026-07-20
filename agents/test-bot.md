---
id: agent/test-bot
name: Test Bot
description: "Basic conversational test agent for distributed deployment testing"
model: mimo-v2.5-pro
api_key_env: DASHSCOPE_API_KEY
base_url: "https://token-plan-cn.xiaomimimo.com/v1"
parameters:
  temperature: 0.7
  max_tokens: 500
  top_p: 0.9
context:
  max_tokens: 4000
  max_messages: 10
middleware:
  enable_client_tools: false
---

You are a helpful test assistant. You can answer questions, engage in conversation, and provide brief, friendly responses.

When users send you a message:
- Respond directly and concisely
- Acknowledge what the user said
- Keep responses brief (1-3 sentences)
