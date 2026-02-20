package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"alfred-ai/internal/domain"
)

// Calendar data types.

// CalendarInfo describes a calendar.
type CalendarInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Primary bool   `json:"primary,omitempty"`
}

// ListEventsOpts controls event listing.
type ListEventsOpts struct {
	TimeMin string `json:"time_min,omitempty"` // ISO 8601
	TimeMax string `json:"time_max,omitempty"` // ISO 8601
	Page    int    `json:"page,omitempty"`
	PerPage int    `json:"per_page,omitempty"`
}

// CalendarEvent describes an event.
type CalendarEvent struct {
	ID          string   `json:"id"`
	CalendarID  string   `json:"calendar_id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Location    string   `json:"location,omitempty"`
	Start       string   `json:"start"` // ISO 8601
	End         string   `json:"end"`   // ISO 8601
	Attendees   []string `json:"attendees,omitempty"`
	AllDay      bool     `json:"all_day,omitempty"`
}

// CreateEventInput is the input for creating an event.
type CreateEventInput struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Location    string   `json:"location,omitempty"`
	Start       string   `json:"start"`
	End         string   `json:"end"`
	Attendees   []string `json:"attendees,omitempty"`
	AllDay      bool     `json:"all_day,omitempty"`
}

// UpdateEventInput is the input for updating an event.
type UpdateEventInput struct {
	Title       *string  `json:"title,omitempty"`
	Description *string  `json:"description,omitempty"`
	Location    *string  `json:"location,omitempty"`
	Start       *string  `json:"start,omitempty"`
	End         *string  `json:"end,omitempty"`
	Attendees   []string `json:"attendees,omitempty"`
}

// CalendarBackend abstracts calendar operations.
type CalendarBackend interface {
	ListCalendars(ctx context.Context) ([]CalendarInfo, error)
	ListEvents(ctx context.Context, calendarID string, opts ListEventsOpts) ([]CalendarEvent, error)
	GetEvent(ctx context.Context, calendarID, eventID string) (*CalendarEvent, error)
	CreateEvent(ctx context.Context, calendarID string, event CreateEventInput) (*CalendarEvent, error)
	UpdateEvent(ctx context.Context, calendarID, eventID string, update UpdateEventInput) (*CalendarEvent, error)
	DeleteEvent(ctx context.Context, calendarID, eventID string) error
}

// MockCalendarBackend is a no-op backend for testing/development.
type MockCalendarBackend struct {
	calendars []CalendarInfo
	events    map[string][]CalendarEvent // key: calendarID
	nextID    int
}

// NewMockCalendarBackend creates a mock calendar backend.
func NewMockCalendarBackend() *MockCalendarBackend {
	return &MockCalendarBackend{
		calendars: []CalendarInfo{{ID: "primary", Name: "Primary Calendar", Primary: true}},
		events:    make(map[string][]CalendarEvent),
		nextID:    1,
	}
}

func (m *MockCalendarBackend) ListCalendars(_ context.Context) ([]CalendarInfo, error) {
	return m.calendars, nil
}

func (m *MockCalendarBackend) ListEvents(_ context.Context, calendarID string, _ ListEventsOpts) ([]CalendarEvent, error) {
	return m.events[calendarID], nil
}

func (m *MockCalendarBackend) GetEvent(_ context.Context, calendarID, eventID string) (*CalendarEvent, error) {
	for _, ev := range m.events[calendarID] {
		if ev.ID == eventID {
			return &ev, nil
		}
	}
	return nil, fmt.Errorf("event %q not found", eventID)
}

func (m *MockCalendarBackend) CreateEvent(_ context.Context, calendarID string, input CreateEventInput) (*CalendarEvent, error) {
	ev := CalendarEvent{
		ID:          fmt.Sprintf("evt-%d", m.nextID),
		CalendarID:  calendarID,
		Title:       input.Title,
		Description: input.Description,
		Location:    input.Location,
		Start:       input.Start,
		End:         input.End,
		Attendees:   input.Attendees,
		AllDay:      input.AllDay,
	}
	m.nextID++
	m.events[calendarID] = append(m.events[calendarID], ev)
	return &ev, nil
}

func (m *MockCalendarBackend) UpdateEvent(_ context.Context, calendarID, eventID string, update UpdateEventInput) (*CalendarEvent, error) {
	events := m.events[calendarID]
	for i := range events {
		if events[i].ID == eventID {
			if update.Title != nil {
				events[i].Title = *update.Title
			}
			if update.Description != nil {
				events[i].Description = *update.Description
			}
			if update.Location != nil {
				events[i].Location = *update.Location
			}
			if update.Start != nil {
				events[i].Start = *update.Start
			}
			if update.End != nil {
				events[i].End = *update.End
			}
			if update.Attendees != nil {
				events[i].Attendees = update.Attendees
			}
			m.events[calendarID] = events
			return &events[i], nil
		}
	}
	return nil, fmt.Errorf("event %q not found", eventID)
}

func (m *MockCalendarBackend) DeleteEvent(_ context.Context, calendarID, eventID string) error {
	events := m.events[calendarID]
	for i, ev := range events {
		if ev.ID == eventID {
			m.events[calendarID] = append(events[:i], events[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("event %q not found", eventID)
}

// CalendarTool provides calendar operations to the LLM.
type CalendarTool struct {
	backend CalendarBackend
	logger  *slog.Logger
}

// NewCalendarTool creates a calendar tool. If backend is nil, a MockCalendarBackend is used.
func NewCalendarTool(backend CalendarBackend, timeout time.Duration, logger *slog.Logger) *CalendarTool {
	if backend == nil {
		backend = NewMockCalendarBackend()
	}
	return &CalendarTool{backend: backend, logger: logger}
}

func (t *CalendarTool) Name() string { return "calendar" }
func (t *CalendarTool) Description() string {
	return "Manage calendars and events: list calendars, list/get/create/update/delete events. " +
		"Times use ISO 8601 format."
}

func (t *CalendarTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["list_calendars", "list_events", "get_event", "create_event", "update_event", "delete_event"],
					"description": "The calendar action to perform"
				},
				"calendar_id": {
					"type": "string",
					"description": "Calendar ID (required for event operations)"
				},
				"event_id": {
					"type": "string",
					"description": "Event ID (for get/update/delete)"
				},
				"title": {
					"type": "string",
					"description": "Event title"
				},
				"description": {
					"type": "string",
					"description": "Event description"
				},
				"location": {
					"type": "string",
					"description": "Event location"
				},
				"start": {
					"type": "string",
					"description": "Start time (ISO 8601)"
				},
				"end": {
					"type": "string",
					"description": "End time (ISO 8601)"
				},
				"attendees": {
					"type": "array",
					"items": {"type": "string"},
					"description": "Attendee email addresses"
				},
				"all_day": {
					"type": "boolean",
					"description": "Whether this is an all-day event"
				},
				"time_min": {
					"type": "string",
					"description": "Filter events starting after this time (ISO 8601)"
				},
				"time_max": {
					"type": "string",
					"description": "Filter events starting before this time (ISO 8601)"
				},
				"page": {
					"type": "integer",
					"description": "Page number"
				},
				"per_page": {
					"type": "integer",
					"description": "Results per page"
				}
			},
			"required": ["action"]
		}`),
	}
}

type calendarParams struct {
	Action      string   `json:"action"`
	CalendarID  string   `json:"calendar_id,omitempty"`
	EventID     string   `json:"event_id,omitempty"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Location    string   `json:"location,omitempty"`
	Start       string   `json:"start,omitempty"`
	End         string   `json:"end,omitempty"`
	Attendees   []string `json:"attendees,omitempty"`
	AllDay      bool     `json:"all_day,omitempty"`
	TimeMin     string   `json:"time_min,omitempty"`
	TimeMax     string   `json:"time_max,omitempty"`
	Page        int      `json:"page,omitempty"`
	PerPage     int      `json:"per_page,omitempty"`
}

func (t *CalendarTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.calendar", t.logger, params,
		Dispatch(func(p calendarParams) string { return p.Action }, ActionMap[calendarParams]{
			"list_calendars": t.handleListCalendars,
			"list_events":    t.handleListEvents,
			"get_event":      t.handleGetEvent,
			"create_event":   t.handleCreateEvent,
			"update_event":   t.handleUpdateEvent,
			"delete_event":   t.handleDeleteEvent,
		}),
	)
}

func validateISO8601(name, value string) error {
	if value == "" {
		return nil
	}
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		return fmt.Errorf("'%s' must be a valid ISO 8601 timestamp (e.g. 2025-01-01T10:00:00Z): %v", name, err)
	}
	return nil
}

func (t *CalendarTool) handleListCalendars(ctx context.Context, _ calendarParams) (any, error) {
	cals, err := t.backend.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	if len(cals) == 0 {
		return TextResult("No calendars found."), nil
	}
	return cals, nil
}

func (t *CalendarTool) handleListEvents(ctx context.Context, p calendarParams) (any, error) {
	if err := RequireField("calendar_id", p.CalendarID); err != nil {
		return nil, err
	}
	if err := validateISO8601("time_min", p.TimeMin); err != nil {
		return nil, err
	}
	if err := validateISO8601("time_max", p.TimeMax); err != nil {
		return nil, err
	}
	events, err := t.backend.ListEvents(ctx, p.CalendarID, ListEventsOpts{
		TimeMin: p.TimeMin, TimeMax: p.TimeMax, Page: p.Page, PerPage: p.PerPage,
	})
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return TextResult("No events found."), nil
	}
	return events, nil
}

func (t *CalendarTool) handleGetEvent(ctx context.Context, p calendarParams) (any, error) {
	if err := RequireFields("calendar_id", p.CalendarID, "event_id", p.EventID); err != nil {
		return nil, err
	}
	return t.backend.GetEvent(ctx, p.CalendarID, p.EventID)
}

func (t *CalendarTool) handleCreateEvent(ctx context.Context, p calendarParams) (any, error) {
	if err := RequireFields("calendar_id", p.CalendarID, "title", p.Title, "start", p.Start, "end", p.End); err != nil {
		return nil, err
	}
	if err := validateISO8601("start", p.Start); err != nil {
		return nil, err
	}
	if err := validateISO8601("end", p.End); err != nil {
		return nil, err
	}
	return t.backend.CreateEvent(ctx, p.CalendarID, CreateEventInput{
		Title:       p.Title,
		Description: p.Description,
		Location:    p.Location,
		Start:       p.Start,
		End:         p.End,
		Attendees:   p.Attendees,
		AllDay:      p.AllDay,
	})
}

func (t *CalendarTool) handleUpdateEvent(ctx context.Context, p calendarParams) (any, error) {
	if err := RequireFields("calendar_id", p.CalendarID, "event_id", p.EventID); err != nil {
		return nil, err
	}
	if err := validateISO8601("start", p.Start); err != nil {
		return nil, err
	}
	if err := validateISO8601("end", p.End); err != nil {
		return nil, err
	}
	update := UpdateEventInput{Attendees: p.Attendees}
	if p.Title != "" {
		update.Title = &p.Title
	}
	if p.Description != "" {
		update.Description = &p.Description
	}
	if p.Location != "" {
		update.Location = &p.Location
	}
	if p.Start != "" {
		update.Start = &p.Start
	}
	if p.End != "" {
		update.End = &p.End
	}
	return t.backend.UpdateEvent(ctx, p.CalendarID, p.EventID, update)
}

func (t *CalendarTool) handleDeleteEvent(ctx context.Context, p calendarParams) (any, error) {
	if err := RequireFields("calendar_id", p.CalendarID, "event_id", p.EventID); err != nil {
		return nil, err
	}
	if err := t.backend.DeleteEvent(ctx, p.CalendarID, p.EventID); err != nil {
		return nil, err
	}
	t.logger.Debug("event deleted", "calendar_id", p.CalendarID, "event_id", p.EventID)
	return TextResult(fmt.Sprintf("Event %q deleted from calendar %q", p.EventID, p.CalendarID)), nil
}
