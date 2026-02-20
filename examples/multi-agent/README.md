# Multi-Agent Example

Multiple specialized agents with keyword-based routing.

## Prerequisites

- OpenAI API key

## Setup

```bash
export OPENAI_API_KEY=sk-...
../../alfred-ai --config=config.yaml
```

## Usage

The router automatically selects the appropriate agent based on keywords:

```
> Help me debug this Python code
```
→ Routes to **coder** agent

```
> Write a blog post about AI
```
→ Routes to **writer** agent

```
> What's the weather like?
```
→ Routes to **general** agent (default)

## What's Included

- Keyword-based routing
- 3 specialized agents (general, coder, writer)
- Shared memory across agents
- CLI interface
