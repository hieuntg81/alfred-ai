//go:build edge

package main

// edgeBuild is true when the binary is built with the `edge` tag.
// Edge builds exclude heavy tools (browser, canvas, voice_call, camera, location)
// and only register lightweight LLM providers (Ollama).
const edgeBuild = true
