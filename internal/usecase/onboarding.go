package usecase

import "sync"

// OnboardingHelper manages first-contact tracking and welcome messages.
type OnboardingHelper struct {
	mu            sync.RWMutex
	firstContacts map[string]bool // sessionKey → contacted
}

// NewOnboardingHelper creates a new onboarding helper.
func NewOnboardingHelper() *OnboardingHelper {
	return &OnboardingHelper{
		firstContacts: make(map[string]bool),
	}
}

// IsFirstContact returns true if this is the first time contacting this session.
func (o *OnboardingHelper) IsFirstContact(sessionKey string) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return !o.firstContacts[sessionKey]
}

// MarkContacted marks a session as contacted (no longer first contact).
func (o *OnboardingHelper) MarkContacted(sessionKey string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.firstContacts[sessionKey] = true
}

// Reset resets the first-contact status for a session (e.g., after /clear).
func (o *OnboardingHelper) Reset(sessionKey string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.firstContacts, sessionKey)
}

// Welcome message templates for each channel
const (
	welcomeCLI = `👋 Welcome to alfred-ai!

I'm your AI assistant with privacy-first design and long-term memory.

Quick Tips:
• Type /help to see available commands
• I remember our conversations across sessions
• Type /privacy to understand how your data is used

What would you like to talk about?`

	welcomeTelegram = `👋 Welcome to alfred-ai!

I'm an AI assistant that can:
✨ Answer questions and have conversations
🧠 Remember what we talk about across sessions
🔧 Help with tasks using tools

Try these to get started:
• "Hello! Who are you?"
• "What can you do?"
• "Remember my favorite color is blue"

Commands:
/help - Show all commands
/clear - Start fresh conversation
/privacy - How your data is used`

	welcomeDiscord = `👋 Welcome to alfred-ai!

I'm an AI assistant with:
✨ Multi-LLM support (GPT-4, Claude, Gemini)
🧠 Long-term memory across conversations
🔒 Privacy-first design with encryption
🔧 Tool execution capabilities

**Getting Started:**
• Just chat naturally or mention @alfred-ai
• Use /help to see all commands
• I remember context across sessions

**Popular Commands:**
` + "`/help`" + ` - Show command list
` + "`/clear`" + ` - Clear conversation
` + "`/privacy`" + ` - Data usage policy

What can I help you with?`

	welcomeSlack = `👋 Welcome to alfred-ai!

I'm your team's AI assistant with enterprise-grade privacy.

**Capabilities:**
• Answer questions using AI (GPT-4/Claude/Gemini)
• Remember context across conversations
• Execute tasks with built-in tools
• Secure by default (encryption, sandboxing, audit logs)

**How to Use:**
• Direct message: Just chat normally
• In channels: Mention @alfred-ai
• Commands: Type /help for full list

**Quick Commands:**
` + "`/help`" + ` - Show all commands
` + "`/clear`" + ` - Clear conversation history
` + "`/privacy`" + ` - Data usage policy
` + "`/status`" + ` - Bot health (admins only)

Let me know how I can help!`

	welcomeHTTP = `👋 Welcome to alfred-ai API!

You're connected to a privacy-first AI agent.

Capabilities:
- Multi-LLM support (OpenAI, Anthropic, Google)
- Long-term memory across sessions
- Tool execution (filesystem, shell, delegation)
- Enterprise security (encryption, sandbox, audit)

API Endpoints:
- POST /api/v1/chat - Send messages
- GET /api/v1/health - Health check

Session ID: Use consistent sessionID for conversation continuity.

See /docs for full API documentation.`

	welcomeDefault = `👋 Welcome to alfred-ai!

I'm an AI assistant with privacy-first design.
Type /help for available commands.`
)

// GetWelcomeMessage returns the appropriate welcome message for a channel.
func GetWelcomeMessage(channel string) string {
	switch channel {
	case "cli":
		return welcomeCLI
	case "telegram":
		return welcomeTelegram
	case "discord":
		return welcomeDiscord
	case "slack":
		return welcomeSlack
	case "http":
		return welcomeHTTP
	default:
		return welcomeDefault
	}
}

// GetHintForMilestone returns a hint message for specific message count milestones.
func GetHintForMilestone(msgCount int) string {
	switch msgCount {
	case 5:
		return "💡 Tip: I can remember things! Try saying 'Remember that I like pizza'"
	case 10:
		return "💡 Tip: You can export all our conversations with /export"
	case 20:
		return "💡 Tip: Use /clear anytime to start a fresh conversation"
	case 50:
		return "💡 Power user tip: I have tool execution capabilities. Ask me to search the web, read files, or run commands!"
	case 100:
		return "🎉 Power user unlocked! You've had 100+ messages with me. Check out advanced features in the documentation!"
	default:
		return ""
	}
}
