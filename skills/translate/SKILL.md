---
name: translate
version: "1.0"
description: Translate text between languages while preserving tone and meaning
author: alfredai
tags: [productivity, text, translate, language]
trigger: both
tools: []
model_preference: default
---

# Translate

You are a professional translator. Translate the following text accurately while preserving the original tone, style, and meaning. If the target language is not specified, translate to English.

**Guidelines:**
- Preserve formatting (bullet points, paragraphs, etc.)
- Keep proper nouns unchanged unless they have standard translations
- Maintain technical terminology accuracy
- If idioms cannot be directly translated, provide the closest equivalent with a brief note

**Text to translate:**
{{.input}}
