package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	userAgent    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_7_2) AppleWebKit/537.36"
	maxRedirects = 5
)

// --- web_search ---

// WebSearchTool searches the web using the Brave Search API.
type WebSearchTool struct {
	apiKey     string
	maxResults int
	client     *http.Client
}

// NewWebSearchTool creates a new web search tool.
func NewWebSearchTool(apiKey string) *WebSearchTool {
	return &WebSearchTool{
		apiKey:     apiKey,
		maxResults: 5,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *WebSearchTool) Name() string        { return "web_search" }
func (t *WebSearchTool) Description() string { return "Search the web. Returns titles, URLs, and snippets." }
func (t *WebSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Search query"},
			"count": map[string]any{"type": "integer", "description": "Results (1-10)", "minimum": 1, "maximum": 10},
		},
		"required": []string{"query"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	query, err := requireStringParam(params, "query")
	if err != nil {
		return "", err
	}
	if t.apiKey == "" {
		return "Error: BRAVE_API_KEY not configured", nil
	}

	count := t.maxResults
	if c, ok := params["count"].(float64); ok {
		count = int(c)
	}
	if count < 1 {
		count = 1
	}
	if count > 10 {
		count = 10
	}

	reqURL := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		url.QueryEscape(query), count)

	req, _ := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", t.apiKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Sprintf("Error: Brave API returned HTTP %d", resp.StatusCode), nil
	}

	var data struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return fmt.Sprintf("Error parsing results: %s", err), nil
	}

	results := data.Web.Results
	if len(results) == 0 {
		return fmt.Sprintf("No results for: %s", query), nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Results for: %s\n", query))
	for i, item := range results {
		if i >= count {
			break
		}
		lines = append(lines, fmt.Sprintf("%d. %s\n   %s", i+1, item.Title, item.URL))
		if item.Description != "" {
			lines = append(lines, fmt.Sprintf("   %s", item.Description))
		}
	}
	return strings.Join(lines, "\n"), nil
}

// --- web_fetch ---

// WebFetchTool fetches a URL and extracts readable content.
type WebFetchTool struct {
	maxChars int
	client   *http.Client
}

// NewWebFetchTool creates a new web fetch tool.
func NewWebFetchTool() *WebFetchTool {
	return &WebFetchTool{
		maxChars: 50000,
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return fmt.Errorf("too many redirects (max %d)", maxRedirects)
				}
				return nil
			},
		},
	}
}

func (t *WebFetchTool) Name() string        { return "web_fetch" }
func (t *WebFetchTool) Description() string { return "Fetch URL and extract readable content (HTML to text/markdown)." }
func (t *WebFetchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":         map[string]any{"type": "string", "description": "URL to fetch"},
			"extractMode": map[string]any{"type": "string", "enum": []string{"markdown", "text"}, "description": "Output format"},
			"maxChars":    map[string]any{"type": "integer", "minimum": 100, "description": "Max content length"},
		},
		"required": []string{"url"},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	rawURL, err := requireStringParam(params, "url")
	if err != nil {
		return "", err
	}

	extractMode := getStringParam(params, "extractMode")
	if extractMode == "" {
		extractMode = "markdown"
	}

	maxChars := t.maxChars
	if mc, ok := params["maxChars"].(float64); ok && int(mc) >= 100 {
		maxChars = int(mc)
	}

	// Validate URL
	if ok, errMsg := validateURL(rawURL); !ok {
		return jsonResult(map[string]any{"error": "URL validation failed: " + errMsg, "url": rawURL}), nil
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	req.Header.Set("User-Agent", userAgent)

	resp, err := t.client.Do(req)
	if err != nil {
		return jsonResult(map[string]any{"error": err.Error(), "url": rawURL}), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return jsonResult(map[string]any{"error": err.Error(), "url": rawURL}), nil
	}

	if resp.StatusCode >= 400 {
		return jsonResult(map[string]any{
			"error":  fmt.Sprintf("HTTP %d", resp.StatusCode),
			"url":    rawURL,
			"status": resp.StatusCode,
		}), nil
	}

	ctype := resp.Header.Get("Content-Type")
	text := string(body)
	extractor := "raw"

	switch {
	case strings.Contains(ctype, "application/json"):
		// Pretty-print JSON
		var j any
		if json.Unmarshal(body, &j) == nil {
			if pretty, err := json.MarshalIndent(j, "", "  "); err == nil {
				text = string(pretty)
			}
		}
		extractor = "json"

	case strings.Contains(ctype, "text/html") || isHTMLContent(text):
		title, article := extractReadable(text)
		if extractMode == "markdown" {
			text = htmlToMarkdown(article)
		} else {
			text = stripTags(article)
		}
		if title != "" {
			text = "# " + title + "\n\n" + text
		}
		extractor = "readability"
	}

	truncated := len(text) > maxChars
	if truncated {
		text = text[:maxChars]
	}

	return jsonResult(map[string]any{
		"url":       rawURL,
		"finalUrl":  resp.Request.URL.String(),
		"status":    resp.StatusCode,
		"extractor": extractor,
		"truncated": truncated,
		"length":    len(text),
		"text":      text,
	}), nil
}

// --- HTML processing helpers ---

var (
	reScript    = regexp.MustCompile(`(?is)<script[\s\S]*?</script>`)
	reStyle     = regexp.MustCompile(`(?is)<style[\s\S]*?</style>`)
	reNav       = regexp.MustCompile(`(?is)<(?:nav|header|footer|aside)[\s\S]*?</(?:nav|header|footer|aside)>`)
	reTag       = regexp.MustCompile(`<[^>]+>`)
	reSpaces    = regexp.MustCompile(`[ \t]+`)
	reNewlines  = regexp.MustCompile(`\n{3,}`)
	reTitle     = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reArticle   = regexp.MustCompile(`(?is)<(?:article|main)[^>]*>([\s\S]*?)</(?:article|main)>`)
	reBody      = regexp.MustCompile(`(?is)<body[^>]*>([\s\S]*?)</body>`)
	reLink      = regexp.MustCompile(`(?is)<a\s+[^>]*href=["']([^"']+)["'][^>]*>([\s\S]*?)</a>`)
	reHeading   = regexp.MustCompile(`(?is)<h([1-6])[^>]*>([\s\S]*?)</h[1-6]>`)
	reListItem  = regexp.MustCompile(`(?is)<li[^>]*>([\s\S]*?)</li>`)
	reBlockEnd  = regexp.MustCompile(`(?is)</(p|div|section|article)>`)
	reLineBreak = regexp.MustCompile(`(?is)<(br|hr)\s*/?>`)
)

func validateURL(rawURL string) (bool, string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false, err.Error()
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false, fmt.Sprintf("Only http/https allowed, got '%s'", u.Scheme)
	}
	if u.Host == "" {
		return false, "Missing domain"
	}
	return true, ""
}

func isHTMLContent(text string) bool {
	prefix := text
	if len(prefix) > 256 {
		prefix = prefix[:256]
	}
	lower := strings.ToLower(prefix)
	return strings.HasPrefix(lower, "<!doctype") || strings.HasPrefix(lower, "<html")
}

// extractReadable extracts the title and main content from HTML.
// A simplified readability implementation: prefer <article>/<main>, fall back to <body>.
func extractReadable(rawHTML string) (title, content string) {
	// Extract title
	if m := reTitle.FindStringSubmatch(rawHTML); len(m) > 1 {
		title = strings.TrimSpace(stripTags(m[1]))
	}

	// Try <article> or <main> first
	if m := reArticle.FindStringSubmatch(rawHTML); len(m) > 1 {
		content = m[1]
	} else if m := reBody.FindStringSubmatch(rawHTML); len(m) > 1 {
		content = m[1]
	} else {
		content = rawHTML
	}

	// Remove noise elements
	content = reScript.ReplaceAllString(content, "")
	content = reStyle.ReplaceAllString(content, "")
	content = reNav.ReplaceAllString(content, "")

	return title, content
}

func stripTags(s string) string {
	s = reScript.ReplaceAllString(s, "")
	s = reStyle.ReplaceAllString(s, "")
	s = reTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return normalizeWhitespace(s)
}

func htmlToMarkdown(s string) string {
	// Links: <a href="url">text</a> → [text](url)
	s = reLink.ReplaceAllStringFunc(s, func(match string) string {
		m := reLink.FindStringSubmatch(match)
		if len(m) > 2 {
			return fmt.Sprintf("[%s](%s)", stripTags(m[2]), m[1])
		}
		return match
	})

	// Headings: <h1>text</h1> → # text
	s = reHeading.ReplaceAllStringFunc(s, func(match string) string {
		m := reHeading.FindStringSubmatch(match)
		if len(m) > 2 {
			level := m[1][0] - '0'
			return "\n" + strings.Repeat("#", int(level)) + " " + stripTags(m[2]) + "\n"
		}
		return match
	})

	// List items: <li>text</li> → - text
	s = reListItem.ReplaceAllStringFunc(s, func(match string) string {
		m := reListItem.FindStringSubmatch(match)
		if len(m) > 1 {
			return "\n- " + stripTags(m[1])
		}
		return match
	})

	// Block-level endings → double newline
	s = reBlockEnd.ReplaceAllString(s, "\n\n")
	s = reLineBreak.ReplaceAllString(s, "\n")

	return normalizeWhitespace(stripTags(s))
}

func normalizeWhitespace(s string) string {
	s = reSpaces.ReplaceAllString(s, " ")
	s = reNewlines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func jsonResult(data map[string]any) string {
	b, _ := json.Marshal(data)
	return string(b)
}
