//go:build slack

package channel

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"alfred-ai/internal/domain"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// SlackOption configures the Slack channel.
type SlackOption func(*SlackChannel)

// WithSlackChannels limits the bot to specific channel IDs.
func WithSlackChannels(ids []string) SlackOption {
	return func(s *SlackChannel) {
		s.channelIDs = make(map[string]bool, len(ids))
		for _, id := range ids {
			s.channelIDs[id] = true
		}
	}
}

// WithSlackMentionOnly enables mention-only filtering.
func WithSlackMentionOnly(v bool) SlackOption {
	return func(s *SlackChannel) { s.mentionOnly = v }
}

// SlackChannel implements domain.Channel for Slack via Socket Mode.
type SlackChannel struct {
	botToken    string
	appToken    string
	api         *slack.Client
	socketCli   *socketmode.Client
	handler     domain.MessageHandler
	logger      *slog.Logger
	channelIDs  map[string]bool
	mentionOnly bool
	botUserID   string
	userNames   sync.Map // cache: userID -> display name
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewSlackChannel creates a Slack channel.
func NewSlackChannel(botToken, appToken string, logger *slog.Logger, opts ...SlackOption) *SlackChannel {
	s := &SlackChannel{
		botToken: botToken,
		appToken: appToken,
		logger:   logger,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *SlackChannel) Name() string { return "slack" }

func (s *SlackChannel) Start(ctx context.Context, handler domain.MessageHandler) error {
	s.handler = handler
	s.ctx, s.cancel = context.WithCancel(ctx)

	s.api = slack.New(s.botToken, slack.OptionAppLevelToken(s.appToken))
	s.socketCli = socketmode.New(s.api)

	// Fetch bot user ID for mention detection.
	authResp, err := s.api.AuthTest()
	if err != nil {
		return err
	}
	s.botUserID = authResp.UserID
	s.logger.Info("slack channel started", "bot_user_id", s.botUserID)

	go s.eventLoop()
	go func() {
		if err := s.socketCli.Run(); err != nil {
			s.logger.Error("slack socket mode error", "error", err)
		}
	}()

	return nil
}

func (s *SlackChannel) Stop(_ context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (s *SlackChannel) Send(_ context.Context, msg domain.OutboundMessage) error {
	content := msg.Content
	if msg.IsError {
		content = ":warning: Error: " + content
	}

	opts := []slack.MsgOption{slack.MsgOptionText(content, false)}

	// Thread support.
	if msg.ThreadID != "" {
		opts = append(opts, slack.MsgOptionTS(msg.ThreadID))
	}

	_, _, err := s.api.PostMessage(msg.SessionID, opts...)
	return err
}

func (s *SlackChannel) eventLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case evt := <-s.socketCli.Events:
			switch evt.Type {
			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				s.socketCli.Ack(*evt.Request)

				switch ev := eventsAPIEvent.InnerEvent.Data.(type) {
				case *slackevents.MessageEvent:
					s.handleMessage(ev)
				}
			}
		}
	}
}

// resolveUserName returns a display name for a Slack user ID, using a cache
// to avoid repeated API calls.
func (s *SlackChannel) resolveUserName(userID string) string {
	if v, ok := s.userNames.Load(userID); ok {
		return v.(string)
	}
	info, err := s.api.GetUserInfo(userID)
	if err != nil {
		s.logger.Warn("slack: failed to resolve user name", "user_id", userID, "error", err)
		return userID // fallback to ID
	}
	name := info.RealName
	if name == "" {
		name = info.Name
	}
	s.userNames.Store(userID, name)
	return name
}

func (s *SlackChannel) handleMessage(ev *slackevents.MessageEvent) {
	// Ignore bot messages.
	if ev.User == "" || ev.User == s.botUserID || ev.BotID != "" {
		return
	}

	// Channel filter.
	if len(s.channelIDs) > 0 && !s.channelIDs[ev.Channel] {
		return
	}

	// Mention detection.
	isMention := strings.Contains(ev.Text, "<@"+s.botUserID+">")

	// Mention-only gating.
	if s.mentionOnly && !isMention {
		return
	}

	content := ev.Text
	// Strip bot mention for cleaner processing.
	if isMention {
		content = strings.ReplaceAll(content, "<@"+s.botUserID+">", "")
		content = strings.TrimSpace(content)
	}

	// Handle commands first
	if strings.HasPrefix(content, "/") {
		if s.handleCommand(ev.Channel, content) {
			return // Command handled, don't send to agent
		}
	}

	msg := domain.InboundMessage{
		SessionID:   ev.Channel,
		Content:     content,
		ChannelName: "slack",
		SenderID:    ev.User,
		SenderName:  s.resolveUserName(ev.User),
		IsMention:   isMention,
	}

	if ev.ThreadTimeStamp != "" {
		msg.ThreadID = ev.ThreadTimeStamp
	}

	if err := s.handler(s.ctx, msg); err != nil {
		s.logger.Error("slack handler error", "error", err, "channel", ev.Channel)
	}
}

// handleCommand processes bot commands. Returns true if command was handled.
func (s *SlackChannel) handleCommand(channel, content string) bool {
	fields := strings.Fields(content)
	if len(fields) == 0 {
		return false
	}

	cmd := fields[0]

	switch cmd {
	case "/help":
		_, _, _ = s.api.PostMessage(channel, slack.MsgOptionText(GetHelpText("slack"), false))
		return true
	case "/privacy":
		_, _, _ = s.api.PostMessage(channel, slack.MsgOptionText(GetPrivacyText(), false))
		return true
	default:
		return false // Not a bot command, send to agent
	}
}
