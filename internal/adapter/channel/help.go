package channel

const (
	helpCLI = `Available Commands:

/help               Show this help message
/quit, /exit        Exit alfred-ai
/clear              Clear conversation history (start fresh)
/cancel             Cancel active request
/privacy            Show privacy and data flow information
/export [path]      Export memories to JSON file
/delete <id>|all    Delete memory entry or all entries

Features:
• Long-term memory across sessions
• Multi-LLM support (OpenAI, Anthropic, Gemini)
• Tool execution (filesystem, shell, delegation)
• Privacy-first design with encryption

Tips:
• I remember our conversations, so you don't need to repeat context
• Ask me to "remember" things for long-term storage
• Use natural language - no special syntax required

Documentation: See ./docs/ for detailed guides`

	helpTelegram = `🤖 alfred-ai Commands

**Basic Commands:**
/help - Show this help
/start - Restart conversation
/clear - Clear history (fresh start)
/privacy - Data usage policy

**Memory Commands:**
/export - Export conversation history
/forget <topic> - Ask me to forget specific info

**Features:**
✨ Multi-LLM AI (GPT-4, Claude, Gemini)
🧠 Long-term memory across sessions
🔧 Tool execution capabilities
🔒 Privacy-first with encryption

**Usage Tips:**
• Just chat naturally - no special format needed
• I remember context across sessions
• Ask me to "remember" for long-term storage
• I can use tools: web search, file ops, etc.

**Privacy:**
All conversations are encrypted and stored locally.
Use /privacy for details.`

	helpDiscord = `**alfred-ai Help**

**Commands:**
` + "`/help`" + ` - Show this help
` + "`/clear`" + ` - Clear conversation history
` + "`/privacy`" + ` - Data usage and privacy policy
` + "`/export`" + ` - Export memories (if permitted)
` + "`/status`" + ` - Bot status (admins only)

**Features:**
✨ **Multi-LLM Support** - GPT-4, Claude, Gemini
🧠 **Long-term Memory** - Remembers across sessions
🔧 **Tool Execution** - Web search, files, commands
🔒 **Privacy-First** - Encryption, sandboxing, audit logs

**How to Use:**
• Mention @alfred-ai or DM directly
• Chat naturally - I understand context
• Ask me to remember important info
• I can execute tasks with tools

**Examples:**
• "Remember that I prefer Python for scripting"
• "Search the web for latest AI news"
• "What did we discuss yesterday?"

**Privacy:**
Your data is encrypted and stored locally.
Type ` + "`/privacy`" + ` for full details.`

	helpSlack = `*alfred-ai Help*

*Commands:*
` + "`/help`" + ` - Show this help
` + "`/clear`" + ` - Clear conversation
` + "`/privacy`" + ` - Privacy policy
` + "`/export`" + ` - Export memories
` + "`/status`" + ` - Bot health (admins)

*Features:*
• Multi-LLM AI (OpenAI, Anthropic, Google)
• Long-term memory across sessions
• Tool execution (web, files, shell)
• Enterprise security (encryption, audit)

*How to Use:*
• DM: Chat normally
• Channels: Mention @alfred-ai
• Natural language - no special syntax

*Examples:*
• "Remember our team uses Python and Go"
• "What decisions did we make last week?"
• "Search for competitor analysis on [topic]"

*Privacy:*
All data encrypted and stored locally.
Type ` + "`/privacy`" + ` for details.`

	helpWhatsApp = `🤖 *alfred-ai Commands*

/help - Show this help
/privacy - Data usage policy

*Features:*
✨ Multi-LLM AI
🧠 Long-term memory
🔧 Tool execution
🔒 Privacy-first

*Tips:*
• Chat naturally - no special format needed
• I remember context across sessions`

	helpMatrix = `**alfred-ai Commands**

/help - Show this help
/privacy - Data usage policy

**Features:**
- Multi-LLM AI (OpenAI, Anthropic, Google)
- Long-term memory across sessions
- Tool execution capabilities
- Privacy-first design

**Tips:**
- Chat naturally - no special format needed
- I remember context across sessions`

	helpGoogleChat = `*alfred-ai Help*

*Commands:*
/help - Show this help
/privacy - Data usage and privacy policy

*Features:*
• Multi-LLM AI (OpenAI, Anthropic, Google)
• Long-term memory across sessions
• Tool execution capabilities
• Privacy-first design

*How to Use:*
• Mention @alfred-ai in spaces or DM directly
• Chat naturally - I understand context
• Ask me to remember important info

*Privacy:*
All data encrypted and stored locally.
Type /privacy for full details.`

	helpTeams = `**alfred-ai Help**

**Commands:**
/help - Show this help
/privacy - Data usage and privacy policy

**Features:**
- Multi-LLM AI (OpenAI, Anthropic, Google)
- Long-term memory across sessions
- Tool execution capabilities
- Privacy-first design

**How to Use:**
- Mention @alfred-ai in channels or chat directly
- Chat naturally - I understand context
- Ask me to remember important info

**Privacy:**
All data encrypted and stored locally.
Type /privacy for full details.`

	helpSignal = `alfred-ai Commands

/help - Show this help
/privacy - Data usage policy

Features:
- Multi-LLM AI (OpenAI, Anthropic, Google)
- Long-term memory across sessions
- Tool execution capabilities
- Privacy-first design

Tips:
- Chat naturally - no special format needed
- I remember context across sessions
- Ask me to remember important info`

	helpIRC = `alfred-ai Help

Commands:
/help or !help - Show this help
/privacy or !privacy - Data usage policy

Features:
- Multi-LLM AI (OpenAI, Anthropic, Google)
- Long-term memory across sessions
- Tool execution capabilities
- Privacy-first design

How to Use:
- Mention my nick or DM directly
- Chat naturally - I understand context
- Ask me to remember important info

Privacy:
All data encrypted and stored locally.
Type /privacy or !privacy for details.`

	privacyText = `🔒 Privacy & Data Usage

**What We Collect:**
• Your messages and conversation history
• Information you explicitly ask me to remember
• Tool execution results (when you request actions)

**How We Store Data:**
• All data stored locally on this machine
• Encrypted at rest (if encryption is enabled)
• No data sent to third parties except LLM providers
• LLM providers (OpenAI/Anthropic/Google) process messages per their privacy policies

**Your Control:**
• /clear - Delete conversation history
• /delete <id> - Delete specific memory entries
• /export - Export your data anytime
• All data is yours - you can delete it anytime

**Security Features:**
• Sandboxed tool execution
• Audit logging of all actions
• Encryption of sensitive data
• No tracking or analytics

For technical details, see ./docs/security.md`
)

// GetHelpText returns the appropriate help text for a channel
func GetHelpText(channelType string) string {
	switch channelType {
	case "cli":
		return helpCLI
	case "telegram":
		return helpTelegram
	case "discord":
		return helpDiscord
	case "slack":
		return helpSlack
	case "whatsapp":
		return helpWhatsApp
	case "matrix":
		return helpMatrix
	case "googlechat":
		return helpGoogleChat
	case "teams":
		return helpTeams
	case "signal":
		return helpSignal
	case "irc":
		return helpIRC
	default:
		return helpCLI
	}
}

// GetPrivacyText returns the privacy information text
func GetPrivacyText() string {
	return privacyText
}
