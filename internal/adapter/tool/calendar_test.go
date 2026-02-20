package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

// --- test backend ---

type testCalendarBackend struct {
	calendars []CalendarInfo
	events    map[string][]CalendarEvent
	nextID    int

	listCalendarsErr error
	listEventsErr    error
	getEventErr      error
	createEventErr   error
	updateEventErr   error
	deleteEventErr   error
}

func newTestCalendarBackend() *testCalendarBackend {
	return &testCalendarBackend{
		calendars: []CalendarInfo{{ID: "cal-1", Name: "Main", Primary: true}},
		events:    make(map[string][]CalendarEvent),
		nextID:    1,
	}
}

func (b *testCalendarBackend) ListCalendars(_ context.Context) ([]CalendarInfo, error) {
	if b.listCalendarsErr != nil {
		return nil, b.listCalendarsErr
	}
	return b.calendars, nil
}

func (b *testCalendarBackend) ListEvents(_ context.Context, calID string, _ ListEventsOpts) ([]CalendarEvent, error) {
	if b.listEventsErr != nil {
		return nil, b.listEventsErr
	}
	return b.events[calID], nil
}

func (b *testCalendarBackend) GetEvent(_ context.Context, calID, evtID string) (*CalendarEvent, error) {
	if b.getEventErr != nil {
		return nil, b.getEventErr
	}
	for _, ev := range b.events[calID] {
		if ev.ID == evtID {
			return &ev, nil
		}
	}
	return nil, fmt.Errorf("event %q not found", evtID)
}

func (b *testCalendarBackend) CreateEvent(_ context.Context, calID string, input CreateEventInput) (*CalendarEvent, error) {
	if b.createEventErr != nil {
		return nil, b.createEventErr
	}
	ev := CalendarEvent{
		ID: fmt.Sprintf("evt-%d", b.nextID), CalendarID: calID,
		Title: input.Title, Description: input.Description,
		Start: input.Start, End: input.End,
	}
	b.nextID++
	b.events[calID] = append(b.events[calID], ev)
	return &ev, nil
}

func (b *testCalendarBackend) UpdateEvent(_ context.Context, calID, evtID string, update UpdateEventInput) (*CalendarEvent, error) {
	if b.updateEventErr != nil {
		return nil, b.updateEventErr
	}
	events := b.events[calID]
	for i := range events {
		if events[i].ID == evtID {
			if update.Title != nil {
				events[i].Title = *update.Title
			}
			b.events[calID] = events
			return &events[i], nil
		}
	}
	return nil, fmt.Errorf("event %q not found", evtID)
}

func (b *testCalendarBackend) DeleteEvent(_ context.Context, calID, evtID string) error {
	if b.deleteEventErr != nil {
		return b.deleteEventErr
	}
	events := b.events[calID]
	for i, ev := range events {
		if ev.ID == evtID {
			b.events[calID] = append(events[:i], events[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("event %q not found", evtID)
}

// --- helpers ---

func newTestCalendarTool(t *testing.T) (*CalendarTool, *testCalendarBackend) {
	t.Helper()
	b := newTestCalendarBackend()
	tool := NewCalendarTool(b, 15*time.Second, newTestLogger())
	return tool, b
}

func execCalendarTool(t *testing.T, tool *CalendarTool, params any) *domain.ToolResult {
	t.Helper()
	data, _ := json.Marshal(params)
	result, err := tool.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return result
}

// --- metadata ---

func TestCalendarToolName(t *testing.T) {
	tool, _ := newTestCalendarTool(t)
	if tool.Name() != "calendar" {
		t.Errorf("got %q, want %q", tool.Name(), "calendar")
	}
}

func TestCalendarToolDescription(t *testing.T) {
	tool, _ := newTestCalendarTool(t)
	if tool.Description() == "" {
		t.Error("description should not be empty")
	}
}

func TestCalendarToolSchema(t *testing.T) {
	tool, _ := newTestCalendarTool(t)
	schema := tool.Schema()
	if schema.Name != "calendar" {
		t.Errorf("schema name: got %q, want %q", schema.Name, "calendar")
	}
	var params map[string]any
	if err := json.Unmarshal(schema.Parameters, &params); err != nil {
		t.Fatalf("invalid schema JSON: %v", err)
	}
}

// --- action success tests ---

func TestCalendarToolListCalendars(t *testing.T) {
	tool, _ := newTestCalendarTool(t)
	result := execCalendarTool(t, tool, map[string]any{"action": "list_calendars"})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Main") {
		t.Errorf("expected calendar name: %s", result.Content)
	}
}

func TestCalendarToolListCalendarsEmpty(t *testing.T) {
	tool, backend := newTestCalendarTool(t)
	backend.calendars = nil
	result := execCalendarTool(t, tool, map[string]any{"action": "list_calendars"})
	if !strings.Contains(result.Content, "No calendars") {
		t.Errorf("expected empty message: %s", result.Content)
	}
}

func TestCalendarToolListEvents(t *testing.T) {
	tool, backend := newTestCalendarTool(t)
	backend.events["cal-1"] = []CalendarEvent{
		{ID: "e1", Title: "Meeting", Start: "2025-01-01T10:00:00Z", End: "2025-01-01T11:00:00Z"},
	}
	result := execCalendarTool(t, tool, map[string]any{
		"action": "list_events", "calendar_id": "cal-1",
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Meeting") {
		t.Errorf("expected event: %s", result.Content)
	}
}

func TestCalendarToolListEventsEmpty(t *testing.T) {
	tool, _ := newTestCalendarTool(t)
	result := execCalendarTool(t, tool, map[string]any{
		"action": "list_events", "calendar_id": "cal-1",
	})
	if !strings.Contains(result.Content, "No events") {
		t.Errorf("expected empty message: %s", result.Content)
	}
}

func TestCalendarToolGetEvent(t *testing.T) {
	tool, backend := newTestCalendarTool(t)
	backend.events["cal-1"] = []CalendarEvent{
		{ID: "e1", Title: "Standup"},
	}
	result := execCalendarTool(t, tool, map[string]any{
		"action": "get_event", "calendar_id": "cal-1", "event_id": "e1",
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Standup") {
		t.Errorf("expected event data: %s", result.Content)
	}
}

func TestCalendarToolCreateEvent(t *testing.T) {
	tool, _ := newTestCalendarTool(t)
	result := execCalendarTool(t, tool, map[string]any{
		"action":      "create_event",
		"calendar_id": "cal-1",
		"title":       "New Event",
		"start":       "2025-03-01T10:00:00Z",
		"end":         "2025-03-01T11:00:00Z",
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "New Event") {
		t.Errorf("expected event data: %s", result.Content)
	}
}

func TestCalendarToolUpdateEvent(t *testing.T) {
	tool, backend := newTestCalendarTool(t)
	backend.events["cal-1"] = []CalendarEvent{{ID: "e1", Title: "Old"}}
	result := execCalendarTool(t, tool, map[string]any{
		"action": "update_event", "calendar_id": "cal-1", "event_id": "e1",
		"title": "Updated",
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Updated") {
		t.Errorf("expected updated title: %s", result.Content)
	}
}

func TestCalendarToolDeleteEvent(t *testing.T) {
	tool, backend := newTestCalendarTool(t)
	backend.events["cal-1"] = []CalendarEvent{{ID: "e1", Title: "Bye"}}
	result := execCalendarTool(t, tool, map[string]any{
		"action": "delete_event", "calendar_id": "cal-1", "event_id": "e1",
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "deleted") {
		t.Errorf("expected deleted message: %s", result.Content)
	}
}

// --- validation error tests ---

func TestCalendarToolListEventsMissingCalendarID(t *testing.T) {
	tool, _ := newTestCalendarTool(t)
	result := execCalendarTool(t, tool, map[string]any{"action": "list_events"})
	if !result.IsError {
		t.Error("expected error for missing calendar_id")
	}
}

func TestCalendarToolGetEventMissingFields(t *testing.T) {
	tool, _ := newTestCalendarTool(t)
	result := execCalendarTool(t, tool, map[string]any{
		"action": "get_event", "calendar_id": "cal-1",
	})
	if !result.IsError {
		t.Error("expected error for missing event_id")
	}
}

func TestCalendarToolCreateEventMissingFields(t *testing.T) {
	tool, _ := newTestCalendarTool(t)
	result := execCalendarTool(t, tool, map[string]any{
		"action": "create_event", "calendar_id": "cal-1",
	})
	if !result.IsError {
		t.Error("expected error for missing title/start/end")
	}
}

func TestCalendarToolCreateEventInvalidTime(t *testing.T) {
	tool, _ := newTestCalendarTool(t)
	result := execCalendarTool(t, tool, map[string]any{
		"action": "create_event", "calendar_id": "cal-1",
		"title": "t", "start": "not-a-date", "end": "2025-01-01T10:00:00Z",
	})
	if !result.IsError {
		t.Error("expected error for invalid start time")
	}
	if !strings.Contains(result.Content, "ISO 8601") {
		t.Errorf("expected ISO 8601 message: %s", result.Content)
	}
}

func TestCalendarToolUpdateEventMissingFields(t *testing.T) {
	tool, _ := newTestCalendarTool(t)
	result := execCalendarTool(t, tool, map[string]any{
		"action": "update_event",
	})
	if !result.IsError {
		t.Error("expected error for missing fields")
	}
}

func TestCalendarToolDeleteEventMissingFields(t *testing.T) {
	tool, _ := newTestCalendarTool(t)
	result := execCalendarTool(t, tool, map[string]any{
		"action": "delete_event", "calendar_id": "cal-1",
	})
	if !result.IsError {
		t.Error("expected error for missing event_id")
	}
}

func TestCalendarToolUnknownAction(t *testing.T) {
	tool, _ := newTestCalendarTool(t)
	result := execCalendarTool(t, tool, map[string]any{"action": "bad"})
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestCalendarToolInvalidJSON(t *testing.T) {
	tool, _ := newTestCalendarTool(t)
	result, err := tool.Execute(context.Background(), []byte(`{invalid`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestCalendarToolListEventsInvalidTimeFilter(t *testing.T) {
	tool, _ := newTestCalendarTool(t)
	result := execCalendarTool(t, tool, map[string]any{
		"action": "list_events", "calendar_id": "cal-1", "time_min": "bad",
	})
	if !result.IsError {
		t.Error("expected error for invalid time_min")
	}
}

// --- backend error propagation ---

func TestCalendarToolBackendListCalendarsError(t *testing.T) {
	tool, backend := newTestCalendarTool(t)
	backend.listCalendarsErr = fmt.Errorf("api error")
	result := execCalendarTool(t, tool, map[string]any{"action": "list_calendars"})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

func TestCalendarToolBackendGetEventError(t *testing.T) {
	tool, backend := newTestCalendarTool(t)
	backend.getEventErr = fmt.Errorf("api error")
	result := execCalendarTool(t, tool, map[string]any{
		"action": "get_event", "calendar_id": "c", "event_id": "e",
	})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

func TestCalendarToolBackendCreateEventError(t *testing.T) {
	tool, backend := newTestCalendarTool(t)
	backend.createEventErr = fmt.Errorf("api error")
	result := execCalendarTool(t, tool, map[string]any{
		"action": "create_event", "calendar_id": "c",
		"title": "t", "start": "2025-01-01T10:00:00Z", "end": "2025-01-01T11:00:00Z",
	})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

func TestCalendarToolBackendDeleteEventError(t *testing.T) {
	tool, backend := newTestCalendarTool(t)
	backend.deleteEventErr = fmt.Errorf("api error")
	result := execCalendarTool(t, tool, map[string]any{
		"action": "delete_event", "calendar_id": "c", "event_id": "e",
	})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

func TestCalendarToolNilBackendUsesMock(t *testing.T) {
	tool := NewCalendarTool(nil, 15*time.Second, newTestLogger())
	result := execCalendarTool(t, tool, map[string]any{"action": "list_calendars"})
	if result.IsError {
		t.Fatalf("expected success with mock: %s", result.Content)
	}
}

// --- edge cases ---

func TestCalendarToolGetEventNotFound(t *testing.T) {
	tool, _ := newTestCalendarTool(t)
	result := execCalendarTool(t, tool, map[string]any{
		"action": "get_event", "calendar_id": "cal-1", "event_id": "missing",
	})
	if !result.IsError {
		t.Error("expected not found error")
	}
}

func TestCalendarToolDeleteEventNotFound(t *testing.T) {
	tool, _ := newTestCalendarTool(t)
	result := execCalendarTool(t, tool, map[string]any{
		"action": "delete_event", "calendar_id": "cal-1", "event_id": "missing",
	})
	if !result.IsError {
		t.Error("expected not found error")
	}
}

func TestCalendarToolUpdateEventInvalidTime(t *testing.T) {
	tool, backend := newTestCalendarTool(t)
	backend.events["cal-1"] = []CalendarEvent{{ID: "e1"}}
	result := execCalendarTool(t, tool, map[string]any{
		"action": "update_event", "calendar_id": "cal-1", "event_id": "e1",
		"start": "not-valid",
	})
	if !result.IsError {
		t.Error("expected error for invalid time")
	}
}

// --- fuzz ---

func FuzzCalendarTool_Execute(f *testing.F) {
	f.Add([]byte(`{"action":"list_calendars"}`))
	f.Add([]byte(`{"action":"create_event","calendar_id":"c","title":"t","start":"2025-01-01T10:00:00Z","end":"2025-01-01T11:00:00Z"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`invalid`))

	b := newTestCalendarBackend()
	tool := NewCalendarTool(b, 15*time.Second, newTestLogger())
	f.Fuzz(func(t *testing.T, data []byte) {
		tool.Execute(context.Background(), data)
	})
}
