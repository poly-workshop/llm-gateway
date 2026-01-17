# Tool Calling Support

LLM Gateway provides full support for OpenAI-compatible tool calling (function calling), enabling AI agents and applications to interact with external tools and APIs.

## Overview

Tool calling allows language models to:
- Call external functions/APIs with structured parameters
- Integrate with agent frameworks (Vercel AI SDK, LangChain, etc.)
- Execute actions based on user requests
- Retrieve real-time information

## Quick Start

### Basic Tool Call Request

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{
    "model": "dashscope/qwen-plus",
    "messages": [
      {
        "role": "user",
        "content": "What'\''s the weather in San Francisco?"
      }
    ],
    "tools": [
      {
        "type": "function",
        "function": {
          "name": "get_weather",
          "description": "Get the current weather in a location",
          "parameters": {
            "type": "object",
            "properties": {
              "location": {
                "type": "string",
                "description": "The city and state, e.g. San Francisco, CA"
              },
              "unit": {
                "type": "string",
                "enum": ["celsius", "fahrenheit"]
              }
            },
            "required": ["location"]
          }
        }
      }
    ],
    "tool_choice": "auto"
  }'
```

For complete examples and usage patterns, see the full documentation below.

## API Reference

Tool calling uses the standard `/v1/chat/completions` endpoint with additional parameters.

### Request Parameters

- `tools` (array): List of available tools
- `tool_choice` (string | object): Controls which tool is called
  - `"auto"` (default): Model decides
  - `"none"`: No tool calls
  - `"required"`: Must call a tool
  - Object: Force specific tool

### Response Format

Responses include a `tool_calls` array in the message when the model wants to use a tool.

## See Full Documentation

For complete examples, best practices, and SDK integration guides, see:
- [Full Tool Calling Documentation](TOOL_CALLING.md) (complete guide with examples)

## Quick Examples

### Multi-Turn Conversation

1. User request → Model calls tool
2. Execute tool → Send result back
3. Model generates natural language response

### Streaming

Tool calls work with SSE streaming. Arguments are streamed incrementally.

## Provider Support

- **DashScope**: Full support (qwen-plus, qwen-turbo, qwen-max)
- **OpenRouter**: Depends on underlying model

## Best Practices

- Use clear, specific tool descriptions
- Include parameter descriptions
- Handle errors gracefully
- Execute multiple tool calls in parallel when possible
