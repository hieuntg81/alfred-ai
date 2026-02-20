//go:build discord

package channel

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"alfred-ai/internal/domain"
	"github.com/bwmarrin/discordgo"
)

// DiscordOption configures the Discord channel.
type DiscordOption func(*DiscordChannel)

// WithDiscordGuild limits the bot to a specific guild.
func WithDiscordGuild(guildID string) DiscordOption {
	return func(d *DiscordChannel) { d.guildID = guildID }
}

// WithDiscordChannels limits the bot to specific channel IDs.
func WithDiscordChannels(ids []string) DiscordOption {
	return func(d *DiscordChannel) {
		d.channelIDs = make(map[string]bool, len(ids))
		for _, id := range ids {
			d.channelIDs[id] = true
		}
	}
}

// WithDiscordMentionOnly enables mention-only filtering.
func WithDiscordMentionOnly(v bool) DiscordOption {
	return func(d *DiscordChannel) { d.mentionOnly = v }
}

// DiscordChannel implements domain.Channel for Discord via discordgo.
type DiscordChannel struct {
	token       string
	session     *discordgo.Session
	handler     domain.MessageHandler
	logger      *slog.Logger
	guildID     string
	channelIDs  map[string]bool
	mentionOnly bool
	botUserID   string
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
}

// NewDiscordChannel creates a Discord bot channel.
func NewDiscordChannel(token string, logger *slog.Logger, opts ...DiscordOption) *DiscordChannel {
	d := &DiscordChannel{
		token:  token,
		logger: logger,
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

func (d *DiscordChannel) Name() string { return "discord" }

func (d *DiscordChannel) Start(ctx context.Context, handler domain.MessageHandler) error {
	d.handler = handler
	d.ctx, d.cancel = context.WithCancel(ctx)

	dg, err := discordgo.New("Bot " + d.token)
	if err != nil {
		return err
	}
	d.session = dg
	d.session.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages

	d.session.AddHandler(d.onMessageCreate)

	if err := d.session.Open(); err != nil {
		return err
	}

	d.botUserID = d.session.State.User.ID
	d.logger.Info("discord channel started", "user_id", d.botUserID)
	return nil
}

func (d *DiscordChannel) Stop(_ context.Context) error {
	if d.cancel != nil {
		d.cancel()
	}
	if d.session != nil {
		return d.session.Close()
	}
	return nil
}

func (d *DiscordChannel) Send(_ context.Context, msg domain.OutboundMessage) error {
	content := msg.Content
	if msg.IsError {
		content = "Error: " + content
	}

	// Thread support: if ThreadID is set, send to that thread.
	channelID := msg.SessionID
	if msg.ThreadID != "" {
		channelID = msg.ThreadID
	}

	_, err := d.session.ChannelMessageSend(channelID, content)
	return err
}

func (d *DiscordChannel) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore own messages.
	if m.Author.ID == d.botUserID {
		return
	}

	// Guild filter.
	if d.guildID != "" && m.GuildID != d.guildID {
		return
	}

	// Channel filter.
	if len(d.channelIDs) > 0 && !d.channelIDs[m.ChannelID] {
		return
	}

	// Mention detection.
	isMention := false
	for _, u := range m.Mentions {
		if u.ID == d.botUserID {
			isMention = true
			break
		}
	}

	// Mention-only gating for guild messages.
	if d.mentionOnly && m.GuildID != "" && !isMention {
		return
	}

	content := m.Content
	// Strip bot mention from content for cleaner processing.
	if isMention {
		content = strings.ReplaceAll(content, "<@"+d.botUserID+">", "")
		content = strings.ReplaceAll(content, "<@!"+d.botUserID+">", "")
		content = strings.TrimSpace(content)
	}

	// Handle commands first
	if strings.HasPrefix(content, "/") {
		if d.handleCommand(s, m.ChannelID, content) {
			return // Command handled, don't send to agent
		}
	}

	msg := domain.InboundMessage{
		SessionID:   m.ChannelID,
		Content:     content,
		ChannelName: "discord",
		SenderID:    m.Author.ID,
		SenderName:  m.Author.Username,
		IsMention:   isMention,
	}

	if m.GuildID != "" {
		msg.GroupID = m.GuildID
	}

	// Thread: if message is in a thread, record it.
	if m.Thread != nil {
		msg.ThreadID = m.Thread.ID
	}

	if err := d.handler(d.ctx, msg); err != nil {
		d.logger.Error("discord handler error", "error", err, "channel", m.ChannelID)
	}
}

// handleCommand processes bot commands. Returns true if command was handled.
func (d *DiscordChannel) handleCommand(s *discordgo.Session, channelID, content string) bool {
	fields := strings.Fields(content)
	if len(fields) == 0 {
		return false
	}

	cmd := fields[0]

	switch cmd {
	case "/help":
		_, _ = s.ChannelMessageSend(channelID, GetHelpText("discord"))
		return true
	case "/privacy":
		_, _ = s.ChannelMessageSend(channelID, GetPrivacyText())
		return true
	default:
		return false // Not a bot command, send to agent
	}
}
