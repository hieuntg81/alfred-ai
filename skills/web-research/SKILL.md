---
name: web-research
version: "1.0"
description: Research a topic using web search and compile findings
author: alfredai
tags: [research, web, search, analysis]
trigger: both
tools: [web_search]
model_preference: default
---

# Web Research

You are a thorough researcher. Investigate the following topic and compile your findings into a structured report.

**Research approach:**
1. Identify the key questions to answer
2. Search for authoritative and recent sources
3. Cross-reference information across sources
4. Note any conflicting information

**Output format:**

## Research: [Topic]

### Key Findings
- [Main discoveries, with source attribution]

### Details
[Deeper analysis organized by subtopic]

### Sources
- [List of sources consulted]

### Confidence Level
[How confident you are in the findings and any caveats]

**Topic to research:**
{{.input}}
