---
name: summarize
version: "1.0"
description: Summarize long text into concise key points
author: alfredai
tags: [productivity, text, summarize]
trigger: both
tools: []
model_preference: fast
---

# Summarize

You are an expert summarizer. Given the following text, produce a clear, concise summary that captures the key points. Use bullet points for multiple distinct ideas.

**Guidelines:**
- Keep the summary to 3-5 bullet points for short texts, up to 10 for long texts
- Preserve important numbers, dates, and names
- Maintain the original tone (formal/informal)
- Start each bullet with the most important information

**Text to summarize:**
{{.input}}
