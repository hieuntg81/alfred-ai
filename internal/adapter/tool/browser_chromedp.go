package tool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"alfred-ai/internal/domain"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// ChromeDPConfig holds configuration for the chromedp backend.
type ChromeDPConfig struct {
	// RemoteURL is the CDP WebSocket endpoint for connecting to a remote Chrome.
	// If empty, a local Chrome instance is launched.
	RemoteURL string
	// Headless controls whether a locally launched Chrome runs headless.
	Headless bool
	// Timeout is the per-action timeout.
	Timeout time.Duration
}

// cdpTab holds a chromedp tab context and its cancel function.
type cdpTab struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// ChromeDPBackend implements BrowserBackend using chromedp.
type ChromeDPBackend struct {
	mu            sync.Mutex
	allocCancel   context.CancelFunc
	browserCtx    context.Context
	browserCancel context.CancelFunc
	activeID      string             // target ID of the active tab
	tabs          map[string]*cdpTab // all open tabs
	timeout       time.Duration
	logger        *slog.Logger
	connected     bool
}

// activeTab returns the active tab's context. Caller must hold mu.
func (b *ChromeDPBackend) activeTab() *cdpTab {
	return b.tabs[b.activeID]
}

// withTimeout creates a timeout-derived context from the active tab context.
// Caller must hold mu.
func (b *ChromeDPBackend) withTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(b.activeTab().ctx, b.timeout)
}

// NewChromeDPBackend creates a browser backend using chromedp.
func NewChromeDPBackend(cfg ChromeDPConfig, logger *slog.Logger) (*ChromeDPBackend, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	b := &ChromeDPBackend{
		tabs:    make(map[string]*cdpTab),
		timeout: cfg.Timeout,
		logger:  logger,
	}

	var allocCtx context.Context
	if cfg.RemoteURL != "" {
		allocCtx, b.allocCancel = chromedp.NewRemoteAllocator(
			context.Background(), cfg.RemoteURL,
		)
		logger.Info("chromedp connecting to remote browser", "url", cfg.RemoteURL)
	} else {
		// Copy default options to avoid mutating the package-level slice.
		opts := make([]chromedp.ExecAllocatorOption, len(chromedp.DefaultExecAllocatorOptions))
		copy(opts, chromedp.DefaultExecAllocatorOptions[:])
		opts = append(opts,
			chromedp.Flag("headless", cfg.Headless),
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("no-sandbox", true),
			chromedp.Flag("disable-dev-shm-usage", true),
			chromedp.WindowSize(1280, 720),
		)
		allocCtx, b.allocCancel = chromedp.NewExecAllocator(
			context.Background(), opts...,
		)
		logger.Info("chromedp launching local browser", "headless", cfg.Headless)
	}

	b.browserCtx, b.browserCancel = chromedp.NewContext(allocCtx)

	// Create initial tab.
	tabCtx, tabCancel := chromedp.NewContext(b.browserCtx)

	// Start browser by running an empty action.
	// IMPORTANT: We must NOT wrap tabCtx in context.WithTimeout because
	// chromedp binds the CDP session to the context passed to the first Run.
	// Canceling a derived context would kill the session immediately.
	startDone := make(chan error, 1)
	go func() { startDone <- chromedp.Run(tabCtx) }()
	select {
	case err := <-startDone:
		if err != nil {
			tabCancel()
			b.Close()
			return nil, fmt.Errorf("start browser: %w", err)
		}
	case <-time.After(cfg.Timeout):
		tabCancel()
		b.Close()
		return nil, fmt.Errorf("start browser: timed out after %v", cfg.Timeout)
	}

	// Register the initial tab.
	ct := chromedp.FromContext(tabCtx)
	initialID := string(ct.Target.TargetID)
	b.tabs[initialID] = &cdpTab{ctx: tabCtx, cancel: tabCancel}
	b.activeID = initialID
	b.connected = true

	logger.Info("chromedp browser started")
	return b, nil
}

func (b *ChromeDPBackend) Name() string { return "chromedp" }

func (b *ChromeDPBackend) Navigate(ctx context.Context, url string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	tctx, cancel := b.withTimeout()
	defer cancel()

	return chromedp.Run(tctx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
	)
}

func (b *ChromeDPBackend) GetContent(ctx context.Context, selector string) (*PageContent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	tctx, cancel := b.withTimeout()
	defer cancel()

	domTarget := "document.body"
	if selector != "" {
		domTarget = fmt.Sprintf("document.querySelector(%q)", selector)
	}

	var result string
	if err := chromedp.Run(tctx,
		chromedp.Evaluate(contentExtractionJS(domTarget), &result),
	); err != nil {
		return nil, fmt.Errorf("get content: %w", err)
	}

	var pc PageContent
	if err := json.Unmarshal([]byte(result), &pc); err != nil {
		// Fallback: return raw text.
		pc.Text = result
	}
	return &pc, nil
}

// screenshotQualities is the sequence of JPEG quality levels tried when a
// screenshot exceeds maxScreenshotBase64. Lower quality = smaller file.
var screenshotQualities = []int{80, 60, 40, 20}

func (b *ChromeDPBackend) captureJPEG(ctx context.Context, fullPage bool, quality int) ([]byte, error) {
	var buf []byte
	var action chromedp.Action
	if fullPage {
		action = chromedp.FullScreenshot(&buf, quality)
	} else {
		q := int64(quality)
		action = chromedp.ActionFunc(func(actx context.Context) error {
			data, err := page.CaptureScreenshot().
				WithFormat(page.CaptureScreenshotFormatJpeg).
				WithQuality(q).
				Do(actx)
			if err != nil {
				return err
			}
			buf = data
			return nil
		})
	}
	if err := chromedp.Run(ctx, action); err != nil {
		return nil, err
	}
	return buf, nil
}

func (b *ChromeDPBackend) Screenshot(ctx context.Context, fullPage bool) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	tctx, cancel := b.withTimeout()
	defer cancel()

	// Try progressively lower JPEG quality until the result fits.
	var encoded string
	for _, quality := range screenshotQualities {
		buf, err := b.captureJPEG(tctx, fullPage, quality)
		if err != nil {
			return "", domain.WrapOp("screenshot", err)
		}
		encoded = base64.StdEncoding.EncodeToString(buf)
		if len(encoded) <= maxScreenshotBase64 {
			return encoded, nil
		}
		b.logger.Debug("screenshot too large, reducing quality",
			"quality", quality, "base64_len", len(encoded), "max", maxScreenshotBase64)
	}

	// All quality levels exceeded the limit; return the lowest-quality result
	// so the caller can decide what to do (the image is still valid).
	return encoded, nil
}

func (b *ChromeDPBackend) Click(ctx context.Context, selector string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	tctx, cancel := b.withTimeout()
	defer cancel()

	return chromedp.Run(tctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.Click(selector, chromedp.ByQuery),
	)
}

func (b *ChromeDPBackend) Type(ctx context.Context, selector string, text string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	tctx, cancel := b.withTimeout()
	defer cancel()

	return chromedp.Run(tctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.SendKeys(selector, text, chromedp.ByQuery),
	)
}

func (b *ChromeDPBackend) Evaluate(ctx context.Context, expression string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	tctx, cancel := b.withTimeout()
	defer cancel()

	var result interface{}
	if err := chromedp.Run(tctx,
		chromedp.Evaluate(expression, &result),
	); err != nil {
		return "", domain.WrapOp("evaluate", err)
	}

	switch v := result.(type) {
	case string:
		return v, nil
	case nil:
		return "undefined", nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v), nil
		}
		return string(data), nil
	}
}

func (b *ChromeDPBackend) WaitVisible(ctx context.Context, selector string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	tctx, cancel := b.withTimeout()
	defer cancel()

	return chromedp.Run(tctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
	)
}

func (b *ChromeDPBackend) TabList(ctx context.Context) ([]TabInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	targets, err := chromedp.Targets(b.browserCtx)
	if err != nil {
		return nil, fmt.Errorf("tab list: %w", err)
	}

	var tabs []TabInfo
	for _, t := range targets {
		if t.Type != "page" {
			continue
		}
		tabs = append(tabs, TabInfo{
			TargetID: string(t.TargetID),
			Title:    t.Title,
			URL:      t.URL,
			Active:   string(t.TargetID) == b.activeID,
		})
	}
	return tabs, nil
}

func (b *ChromeDPBackend) TabOpen(ctx context.Context, url string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if url == "" {
		url = "about:blank"
	}

	// Explicitly create a new browser target via CDP, then attach a context.
	// Using target.CreateTarget guarantees a new tab (chromedp.NewContext
	// without WithTargetID may reuse an existing blank target).
	var newTargetID target.ID
	if err := chromedp.Run(b.browserCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			newTargetID, err = target.CreateTarget(url).Do(ctx)
			return err
		}),
	); err != nil {
		return "", fmt.Errorf("tab open: %w", err)
	}

	// Attach a chromedp context to the new target.
	newCtx, newCancel := chromedp.NewContext(b.browserCtx, chromedp.WithTargetID(newTargetID))
	if err := chromedp.Run(newCtx); err != nil {
		newCancel()
		return "", fmt.Errorf("tab open attach: %w", err)
	}

	newID := string(newTargetID)
	b.tabs[newID] = &cdpTab{ctx: newCtx, cancel: newCancel}
	b.activeID = newID

	return newID, nil
}

func (b *ChromeDPBackend) TabClose(ctx context.Context, targetID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	tab, ok := b.tabs[targetID]
	if !ok {
		return fmt.Errorf("tab close: unknown target %s", targetID)
	}

	closingActive := targetID == b.activeID

	// Cancel the tab's context — this is how chromedp v0.14 closes tabs.
	tab.cancel()
	delete(b.tabs, targetID)

	// If the closed tab was the active one, switch to another open tab.
	if closingActive {
		b.activeID = ""
		for id := range b.tabs {
			b.activeID = id
			break
		}
		if b.activeID == "" {
			// No tabs left. Create a fresh one so the backend stays usable.
			newCtx, newCancel := chromedp.NewContext(b.browserCtx)
			if err := chromedp.Run(newCtx); err != nil {
				return fmt.Errorf("tab close: create replacement tab: %w", err)
			}
			ct := chromedp.FromContext(newCtx)
			newID := string(ct.Target.TargetID)
			b.tabs[newID] = &cdpTab{ctx: newCtx, cancel: newCancel}
			b.activeID = newID
		}
	}

	return nil
}

func (b *ChromeDPBackend) TabFocus(ctx context.Context, targetID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.tabs[targetID]; !ok {
		return fmt.Errorf("tab focus: unknown target %s", targetID)
	}

	b.activeID = targetID

	// Activate the target in the browser UI.
	return chromedp.Run(b.browserCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return target.ActivateTarget(target.ID(targetID)).Do(ctx)
		}),
	)
}

func (b *ChromeDPBackend) Status(ctx context.Context) (*BrowserStatus, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	status := &BrowserStatus{
		Connected: b.connected,
		Backend:   b.Name(),
	}

	if b.connected {
		targets, err := chromedp.Targets(b.browserCtx)
		if err == nil {
			for _, t := range targets {
				if t.Type == "page" {
					status.TabCount++
				}
			}
		}

		// Get active tab URL with timeout to avoid hanging.
		tctx, cancel := b.withTimeout()
		defer cancel()
		var url string
		if err := chromedp.Run(tctx, chromedp.Location(&url)); err == nil {
			status.ActiveTabURL = url
		}
	}

	return status, nil
}

func (b *ChromeDPBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.connected = false
	for _, tab := range b.tabs {
		tab.cancel()
	}
	b.tabs = nil
	if b.browserCancel != nil {
		b.browserCancel()
	}
	if b.allocCancel != nil {
		b.allocCancel()
	}
	b.logger.Info("chromedp browser closed")
	return nil
}
