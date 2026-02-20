# Basic CLI Example

Minimal configuration for command-line interaction.

## Prerequisites

- OpenAI API key

## Setup

```bash
export OPENAI_API_KEY=sk-...
../../alfred-ai --config=config.yaml
```

## Usage

Type messages directly:
```
> Hello! How are you?
```

The agent will respond using GPT-4.

## What's Included

- OpenAI GPT-4 provider
- Markdown memory storage
- CLI channel only
