package channel

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"alfred-ai/internal/domain"
)

// IRCOption configures an IRCChannel.
type IRCOption func(*IRCChannel)

// WithIRCPassword sets the server password.
func WithIRCPassword(pw string) IRCOption {
	return func(c *IRCChannel) { c.password = pw }
}

// WithIRCUseTLS enables TLS for the connection.
func WithIRCUseTLS(v bool) IRCOption {
	return func(c *IRCChannel) { c.useTLS = v }
}

// IRCChannel implements domain.Channel for IRC.
type IRCChannel struct {
	server   string
	nick     string
	password string
	channels []string
	useTLS   bool

	handler domain.MessageHandler
	logger  *slog.Logger
	conn    net.Conn
	writer  *bufio.Writer
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.Mutex
}

// NewIRCChannel creates an IRC channel.
func NewIRCChannel(server, nick string, channels []string, logger *slog.Logger, opts ...IRCOption) *IRCChannel {
	c := &IRCChannel{
		server:   server,
		nick:     nick,
		channels: channels,
		logger:   logger,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Name implements domain.Channel.
func (c *IRCChannel) Name() string { return "irc" }

// Start implements domain.Channel.
func (c *IRCChannel) Start(ctx context.Context, handler domain.MessageHandler) error {
	c.handler = handler

	conn, err := c.dial()
	if err != nil {
		return fmt.Errorf("irc connect %s: %w", c.server, err)
	}
	c.conn = conn
	c.writer = bufio.NewWriter(conn)

	ircCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// Register with server.
	if c.password != "" {
		c.sendRaw("PASS " + c.password)
	}
	c.sendRaw("NICK " + c.nick)
	c.sendRaw("USER " + c.nick + " 0 * :" + c.nick)

	c.wg.Add(1)
	go c.readLoop(ircCtx)

	c.logger.Info("irc channel started", "server", c.server, "nick", c.nick, "channels", c.channels)
	return nil
}

// Stop implements domain.Channel.
func (c *IRCChannel) Stop(_ context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		c.sendRaw("QUIT :bye")
		c.conn.Close()
	}
	c.wg.Wait()
	return nil
}

// Send implements domain.Channel.
func (c *IRCChannel) Send(_ context.Context, msg domain.OutboundMessage) error {
	content := msg.Content
	if msg.IsError {
		content = "Error: " + content
	}

	target := msg.SessionID
	if target == "" {
		return fmt.Errorf("irc: session_id (target channel/nick) is required")
	}

	// IRC messages have a max length (~510 bytes). Split if needed.
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		c.sendRaw("PRIVMSG " + target + " :" + line)
	}

	return nil
}

// --- Connection ---

func (c *IRCChannel) dial() (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	if c.useTLS {
		return tls.DialWithDialer(dialer, "tcp", c.server, &tls.Config{
			MinVersion: tls.VersionTLS12,
		})
	}
	return dialer.Dial("tcp", c.server)
}

func (c *IRCChannel) sendRaw(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writer == nil {
		return
	}
	c.writer.WriteString(line + "\r\n")
	c.writer.Flush()
}

// --- Read loop ---

func (c *IRCChannel) readLoop(ctx context.Context) {
	defer c.wg.Done()

	scanner := bufio.NewScanner(c.conn)
	joined := false

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		c.logger.Debug("irc recv", "line", line)

		// Handle PING/PONG keepalive.
		if strings.HasPrefix(line, "PING") {
			c.sendRaw("PONG" + line[4:])
			continue
		}

		// Join channels after receiving RPL_WELCOME (001) or RPL_ENDOFMOTD (376).
		if !joined && (strings.Contains(line, " 001 ") || strings.Contains(line, " 376 ")) {
			for _, ch := range c.channels {
				c.sendRaw("JOIN " + ch)
			}
			joined = true
			continue
		}

		// Parse PRIVMSG.
		c.parseAndDispatch(ctx, line)
	}

	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		c.logger.Error("irc read error", "error", err)
	}
}

func (c *IRCChannel) parseAndDispatch(ctx context.Context, line string) {
	// IRC message format: :nick!user@host PRIVMSG #channel :message
	if !strings.Contains(line, " PRIVMSG ") {
		return
	}

	prefix, rest, ok := parseIRCMessage(line)
	if !ok {
		return
	}

	// Extract sender nick from prefix.
	senderNick := prefix
	if idx := strings.Index(prefix, "!"); idx >= 0 {
		senderNick = prefix[:idx]
	}

	// Ignore our own messages.
	if senderNick == c.nick {
		return
	}

	// Split "PRIVMSG target :message"
	parts := strings.SplitN(rest, " :", 2)
	if len(parts) != 2 {
		return
	}

	target := strings.TrimPrefix(parts[0], "PRIVMSG ")
	text := strings.TrimSpace(parts[1])

	if text == "" {
		return
	}

	// Determine if this is a DM or channel message.
	isDM := !strings.HasPrefix(target, "#") && !strings.HasPrefix(target, "&")

	// Check for mention.
	isMention := strings.Contains(strings.ToLower(text), strings.ToLower(c.nick))

	// Handle commands.
	replyTarget := target
	if isDM {
		replyTarget = senderNick
	}
	if c.handleCommand(ctx, replyTarget, text) {
		return
	}

	sessionID := target
	if isDM {
		sessionID = senderNick
	}

	inbound := domain.InboundMessage{
		SessionID:   sessionID,
		Content:     text,
		ChannelName: "irc",
		SenderID:    senderNick,
		SenderName:  senderNick,
		GroupID:     target,
		IsMention:   isMention,
	}

	if err := c.handler(ctx, inbound); err != nil {
		c.logger.Error("irc handler error", "error", err, "sender", senderNick)
	}
}

func (c *IRCChannel) handleCommand(ctx context.Context, target, content string) bool {
	fields := strings.Fields(content)
	if len(fields) == 0 {
		return false
	}

	switch fields[0] {
	case "/help", "!help":
		c.Send(ctx, domain.OutboundMessage{SessionID: target, Content: GetHelpText("irc")})
		return true
	case "/privacy", "!privacy":
		c.Send(ctx, domain.OutboundMessage{SessionID: target, Content: GetPrivacyText()})
		return true
	default:
		return false
	}
}

// --- IRC message parsing ---

// parseIRCMessage extracts prefix and the rest from a raw IRC line.
// Returns (prefix, rest, ok).
func parseIRCMessage(line string) (string, string, bool) {
	if !strings.HasPrefix(line, ":") {
		return "", "", false
	}
	line = line[1:] // strip leading ":"

	spaceIdx := strings.Index(line, " ")
	if spaceIdx < 0 {
		return "", "", false
	}

	return line[:spaceIdx], line[spaceIdx+1:], true
}
