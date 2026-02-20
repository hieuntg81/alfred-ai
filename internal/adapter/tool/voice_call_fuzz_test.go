package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func FuzzVoiceCallTool(f *testing.F) {
	f.Add(`{"action":"initiate_call","message":"hello","to":"+14155551234"}`)
	f.Add(`{"action":"initiate_call","message":"hello"}`)
	f.Add(`{"action":"end_call","call_id":"vc_123"}`)
	f.Add(`{"action":"get_status","call_id":"vc_123"}`)
	f.Add(`{"action":"continue_call","call_id":"vc_123","message":"hello"}`)
	f.Add(`{"action":"speak_to_user","call_id":"vc_123","message":"hello"}`)
	f.Add(`{"action":"","message":""}`)
	f.Add(`{}`)
	f.Add(`{"action":"initiate_call","to":"not-a-phone","message":"test"}`)
	f.Add(`{"action":"initiate_call","message":"test","mode":"invalid"}`)
	f.Add(`{"action":"unknown"}`)

	f.Fuzz(func(t *testing.T, input string) {
		mock := NewMockVoiceCallBackend()
		store := NewCallStore(5, nil)
		vt := NewVoiceCallTool(mock, store, VoiceCallToolConfig{
			FromNumber:       "+15551234567",
			DefaultTo:        "+15559876543",
			WebhookPublicURL: "https://example.com",
			WebhookPath:      "/voice/webhook",
			StreamPath:       "/voice/stream",
		}, newTestLogger())

		result, err := vt.Execute(context.Background(), json.RawMessage(input))
		if err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}
		if result == nil {
			t.Fatal("Execute returned nil result")
		}
	})
}

func FuzzVoiceCallWebhookParse(f *testing.F) {
	f.Add([]byte("CallSid=CA123&CallStatus=completed"))
	f.Add([]byte("CallSid=CA123&CallStatus=ringing"))
	f.Add([]byte("CallSid=&CallStatus="))
	f.Add([]byte(""))
	f.Add([]byte("invalid=data"))
	f.Add([]byte("CallSid=CA123&CallStatus=in-progress&Direction=outbound-api"))

	f.Fuzz(func(t *testing.T, body []byte) {
		backend := NewMockVoiceCallBackend()
		backend.ParseEvents = nil
		backend.ParseErr = nil

		// Use the real Twilio backend's ParseWebhookEvent for fuzzing.
		twilio := NewTwilioBackend(TwilioBackendConfig{
			AccountSID: "AC_test",
			AuthToken:  "test_token",
		}, newTestLogger())

		events, _, err := twilio.ParseWebhookEvent(context.Background(), WebhookParseRequest{
			Body: body,
		})

		// Should never panic.
		if err != nil {
			return // parse errors are expected for fuzz input
		}

		// If we got events, they should have valid fields.
		for _, e := range events {
			if e.Status == "" {
				t.Error("event has empty status")
			}
		}
	})
}
