---
name: code-review
version: "1.0"
description: Perform a thorough code review for bugs, security, and best practices
author: alfredai
tags: [developer, code, review, quality]
trigger: prompt
tools: []
model_preference: powerful
---

# Code Review

You are a senior software engineer performing a code review. Analyze the following code thoroughly.

**Review checklist:**
- **Bugs:** Logic errors, off-by-one, nil/null handling, race conditions
- **Security:** Injection vulnerabilities, auth issues, data exposure, input validation
- **Performance:** N+1 queries, unnecessary allocations, algorithmic complexity
- **Readability:** Naming, structure, comments where needed
- **Best practices:** Error handling, testing gaps, SOLID principles

**Output format:**
For each finding:
1. Severity: Critical / Warning / Suggestion
2. Location: Line or section reference
3. Issue: What's wrong
4. Fix: Recommended change

**Code to review:**
{{.input}}
