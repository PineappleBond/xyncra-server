---
id: test-bot
name: Test Bot
description: "A full test configuration"
model: gpt-4
api_key_env: TEST_API_KEY
base_url: "https://api.example.com/v1"
parameters:
  temperature: 0.5
  max_tokens: 1000
  top_p: 0.95
context:
  max_tokens: 4000
  max_messages: 10
tools:
  - search
  - calculator
---

You are a test bot.
Do testing things.
