package setup

import (
	"bytes"
	"strings"
	"testing"
)

func TestUIHelper_PrintHeader(t *testing.T) {
	var buf bytes.Buffer
	ui := NewUIHelper(strings.NewReader(""), &buf)

	ui.PrintHeader("Test Header")

	output := buf.String()
	if !strings.Contains(output, "Test Header") {
		t.Errorf("PrintHeader should contain 'Test Header', got: %s", output)
	}
	if !strings.Contains(output, "=") {
		t.Errorf("PrintHeader should contain border characters")
	}
}

func TestUIHelper_PrintSection(t *testing.T) {
	var buf bytes.Buffer
	ui := NewUIHelper(strings.NewReader(""), &buf)

	ui.PrintSection("Title", "This is a description")

	output := buf.String()
	if !strings.Contains(output, "Title") {
		t.Errorf("PrintSection should contain title")
	}
	if !strings.Contains(output, "This is a description") {
		t.Errorf("PrintSection should contain description")
	}
}

func TestUIHelper_PrintSuccess(t *testing.T) {
	var buf bytes.Buffer
	ui := NewUIHelper(strings.NewReader(""), &buf)

	ui.PrintSuccess("Operation succeeded")

	output := buf.String()
	if !strings.Contains(output, "✓") {
		t.Errorf("PrintSuccess should contain checkmark")
	}
	if !strings.Contains(output, "Operation succeeded") {
		t.Errorf("PrintSuccess should contain message")
	}
}

func TestUIHelper_PrintError(t *testing.T) {
	var buf bytes.Buffer
	ui := NewUIHelper(strings.NewReader(""), &buf)

	ui.PrintError("Operation failed")

	output := buf.String()
	if !strings.Contains(output, "✗") {
		t.Errorf("PrintError should contain X mark")
	}
	if !strings.Contains(output, "Operation failed") {
		t.Errorf("PrintError should contain message")
	}
}

func TestUIHelper_PrintProgress(t *testing.T) {
	var buf bytes.Buffer
	ui := NewUIHelper(strings.NewReader(""), &buf)

	ui.PrintProgress(3, 6, "Processing")

	output := buf.String()
	if !strings.Contains(output, "50%") {
		t.Errorf("PrintProgress should show 50%% for step 3/6")
	}
	if !strings.Contains(output, "Processing") {
		t.Errorf("PrintProgress should contain message")
	}
}

func TestUIHelper_AskString(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		defaultVal string
		want       string
	}{
		{
			name:       "user provides value",
			input:      "test value\n",
			defaultVal: "default",
			want:       "test value",
		},
		{
			name:       "user presses enter - use default",
			input:      "\n",
			defaultVal: "default",
			want:       "default",
		},
		{
			name:       "no default, user provides value",
			input:      "custom\n",
			defaultVal: "",
			want:       "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			ui := NewUIHelper(strings.NewReader(tt.input), &buf)

			got, err := ui.AskString("Prompt", tt.defaultVal)
			if err != nil {
				t.Fatalf("AskString returned error: %v", err)
			}

			if got != tt.want {
				t.Errorf("AskString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUIHelper_AskConfirmation(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		defaultYes bool
		want       bool
	}{
		{
			name:       "user types 'y'",
			input:      "y\n",
			defaultYes: false,
			want:       true,
		},
		{
			name:       "user types 'yes'",
			input:      "yes\n",
			defaultYes: false,
			want:       true,
		},
		{
			name:       "user types 'n'",
			input:      "n\n",
			defaultYes: true,
			want:       false,
		},
		{
			name:       "user presses enter - default yes",
			input:      "\n",
			defaultYes: true,
			want:       true,
		},
		{
			name:       "user presses enter - default no",
			input:      "\n",
			defaultYes: false,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			ui := NewUIHelper(strings.NewReader(tt.input), &buf)

			got, err := ui.AskConfirmation("Confirm?", tt.defaultYes)
			if err != nil {
				t.Fatalf("AskConfirmation returned error: %v", err)
			}

			if got != tt.want {
				t.Errorf("AskConfirmation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUIHelper_AskChoice(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		max     int
		want    int
		wantErr bool
	}{
		{
			name:    "valid choice 1",
			input:   "1\n",
			max:     3,
			want:    1,
			wantErr: false,
		},
		{
			name:    "valid choice 3",
			input:   "3\n",
			max:     3,
			want:    3,
			wantErr: false,
		},
		{
			name:    "invalid then valid",
			input:   "5\n2\n",
			max:     3,
			want:    2,
			wantErr: false,
		},
		{
			name:    "zero then valid",
			input:   "0\n1\n",
			max:     3,
			want:    1,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			ui := NewUIHelper(strings.NewReader(tt.input), &buf)

			got, err := ui.AskChoice(tt.max)
			if (err != nil) != tt.wantErr {
				t.Fatalf("AskChoice() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && got != tt.want {
				t.Errorf("AskChoice() = %d, want %d", got, tt.want)
			}
		})
	}
}

func Test_wordWrap(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  int // number of lines
	}{
		{
			name:  "short text - single line",
			text:  "Hello world",
			width: 20,
			want:  1,
		},
		{
			name:  "long text - multiple lines",
			text:  "This is a very long text that should be wrapped into multiple lines",
			width: 20,
			want:  4,
		},
		{
			name:  "exact width",
			text:  "Exactly twenty chars",
			width: 20,
			want:  1,
		},
		{
			name:  "empty text",
			text:  "",
			width: 20,
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wordWrap(tt.text, tt.width)
			if len(got) != tt.want {
				t.Errorf("wordWrap() produced %d lines, want %d", len(got), tt.want)
			}

			// Verify no line exceeds width
			for i, line := range got {
				if len(line) > tt.width {
					t.Errorf("Line %d exceeds width: %q (len=%d, max=%d)", i, line, len(line), tt.width)
				}
			}
		})
	}
}

func Test_progressBar(t *testing.T) {
	tests := []struct {
		name       string
		percentage int
		width      int
		wantLength int
	}{
		{
			name:       "0 percent",
			percentage: 0,
			width:      10,
			wantLength: 10,
		},
		{
			name:       "50 percent",
			percentage: 50,
			width:      10,
			wantLength: 10,
		},
		{
			name:       "100 percent",
			percentage: 100,
			width:      10,
			wantLength: 10,
		},
		{
			name:       "over 100 percent (clamped)",
			percentage: 150,
			width:      10,
			wantLength: 10,
		},
		{
			name:       "negative percent (clamped)",
			percentage: -10,
			width:      10,
			wantLength: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := progressBar(tt.percentage, tt.width)

			// Count runes instead of bytes for accurate length
			gotLen := len([]rune(got))
			if gotLen != tt.wantLength {
				t.Errorf("progressBar() length = %d, want %d", gotLen, tt.wantLength)
			}

			// Verify it contains progress characters
			if !strings.Contains(got, "█") && !strings.Contains(got, "░") {
				t.Errorf("progressBar() should contain progress characters")
			}
		})
	}
}

func TestUIHelper_PrintConversation(t *testing.T) {
	tests := []struct {
		name    string
		role    string
		message string
		wantIcon string
	}{
		{
			name:     "user message",
			role:     "user",
			message:  "Hello",
			wantIcon: "👤",
		},
		{
			name:     "assistant message",
			role:     "assistant",
			message:  "Hi there",
			wantIcon: "🤖",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			ui := NewUIHelper(strings.NewReader(""), &buf)

			ui.PrintConversation(tt.role, tt.message)

			output := buf.String()
			if !strings.Contains(output, tt.message) {
				t.Errorf("PrintConversation should contain message")
			}
			if !strings.Contains(output, tt.wantIcon) {
				t.Errorf("PrintConversation should contain icon %s", tt.wantIcon)
			}
		})
	}
}
