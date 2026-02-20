package tool

import "context"

// BrowserBackend abstracts browser automation operations.
type BrowserBackend interface {
	// Navigate loads a URL in the current tab.
	Navigate(ctx context.Context, url string) error
	// GetContent extracts the page content as AI-friendly text.
	// If selector is non-empty, only that element's subtree is extracted.
	GetContent(ctx context.Context, selector string) (*PageContent, error)
	// Screenshot captures the current viewport as a base64-encoded JPEG.
	// If fullPage is true, the entire scrollable page is captured.
	// Backends should use progressive quality reduction to fit size limits.
	Screenshot(ctx context.Context, fullPage bool) (string, error)
	// Click clicks the element matching the given CSS selector.
	Click(ctx context.Context, selector string) error
	// Type types text into the element matching the given CSS selector.
	Type(ctx context.Context, selector string, text string) error
	// Evaluate executes JavaScript and returns the result as a string.
	Evaluate(ctx context.Context, expression string) (string, error)
	// WaitVisible waits for an element matching the selector to become visible.
	WaitVisible(ctx context.Context, selector string) error
	// TabList returns information about all open tabs.
	TabList(ctx context.Context) ([]TabInfo, error)
	// TabOpen opens a new tab, optionally navigating to a URL.
	// Returns the new tab's target ID.
	TabOpen(ctx context.Context, url string) (string, error)
	// TabClose closes the tab with the given target ID.
	TabClose(ctx context.Context, targetID string) error
	// TabFocus switches to the tab with the given target ID.
	TabFocus(ctx context.Context, targetID string) error
	// Status returns browser connection status information.
	Status(ctx context.Context) (*BrowserStatus, error)
	// Close releases all browser resources.
	Close() error
	// Name returns the backend identifier (e.g. "chromedp").
	Name() string
}

// PageContent holds AI-friendly extracted page content.
type PageContent struct {
	Title string     `json:"title"`
	URL   string     `json:"url"`
	Text  string     `json:"text"`
	Links []PageLink `json:"links,omitempty"`
	Forms []PageForm `json:"forms,omitempty"`
}

// PageLink represents an extracted link from the page.
type PageLink struct {
	Index    int    `json:"index"`
	Text     string `json:"text"`
	Href     string `json:"href"`
	Selector string `json:"selector"`
}

// PageForm represents a form found on the page.
type PageForm struct {
	Index  int         `json:"index"`
	Action string      `json:"action"`
	Fields []FormField `json:"fields,omitempty"`
}

// FormField represents an input field within a form.
type FormField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Placeholder string `json:"placeholder,omitempty"`
	Selector    string `json:"selector"`
}

// TabInfo holds information about a browser tab.
type TabInfo struct {
	TargetID string `json:"target_id"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Active   bool   `json:"active"`
}

// BrowserStatus holds browser connection/health info.
type BrowserStatus struct {
	Connected    bool   `json:"connected"`
	Backend      string `json:"backend"`
	TabCount     int    `json:"tab_count"`
	ActiveTabURL string `json:"active_tab_url,omitempty"`
}
