package domain

import "context"

// MediaType identifies the kind of media attached to a message.
type MediaType string

const (
	MediaTypeImage    MediaType = "image"
	MediaTypeAudio    MediaType = "audio"
	MediaTypeVideo    MediaType = "video"
	MediaTypeFile     MediaType = "file"
	MediaTypeLocation MediaType = "location"
)

// Media represents an attachment on an inbound or outbound message.
type Media struct {
	Type     MediaType `json:"type"`
	URL      string    `json:"url,omitempty"`
	MIMEType string    `json:"mime_type,omitempty"`
	Data     []byte    `json:"data,omitempty"`
	Caption  string    `json:"caption,omitempty"`
}

// InboundMessage is a message received from a channel (user input).
type InboundMessage struct {
	SessionID   string
	Content     string
	ChannelName string

	// Enriched fields — all zero-value safe.
	SenderID   string            `json:"sender_id,omitempty"`
	SenderName string            `json:"sender_name,omitempty"`
	GroupID    string            `json:"group_id,omitempty"`
	ThreadID   string            `json:"thread_id,omitempty"`
	ReplyToID  string            `json:"reply_to_id,omitempty"`
	Media      []Media           `json:"media,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	IsMention  bool              `json:"is_mention,omitempty"`
}

// OutboundMessage is a message sent to a channel (agent response).
type OutboundMessage struct {
	SessionID string
	Content   string
	IsError   bool

	// Enriched fields — all zero-value safe.
	ThreadID  string            `json:"thread_id,omitempty"`
	ReplyToID string            `json:"reply_to_id,omitempty"`
	Media     []Media           `json:"media,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// MessageHandler is a callback the channel invokes when it receives input.
type MessageHandler func(ctx context.Context, msg InboundMessage) error

// Channel is the interface for user-facing I/O adapters.
type Channel interface {
	Start(ctx context.Context, handler MessageHandler) error
	Stop(ctx context.Context) error
	Send(ctx context.Context, msg OutboundMessage) error
	Name() string
}
