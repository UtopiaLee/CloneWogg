package crawler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Result represents the outcome of crawling a single page.
type Result struct {
	URL   string   `json:"url"`
	Title string   `json:"title"`
	Links []string `json:"links"`
	Error string   `json:"error,omitempty"`
}

// Config provides runtime options for the crawler.
type Config struct {
	MaxDepth   int
	MaxWorkers int
	SameHost   bool
	Timeout    time.Duration
}

// Crawler fetches pages starting from a seed URL.
type Crawler struct {
	client  *http.Client
	config  Config
	visited map[string]struct{}
	mu      sync.Mutex
}

// New creates a crawler with sane defaults when values are missing or zero.
func New(cfg Config) *Crawler {
	if cfg.MaxDepth < 0 {
		cfg.MaxDepth = 0
	}
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 4
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}

	return &Crawler{
		client:  &http.Client{Timeout: cfg.Timeout},
		config:  cfg,
		visited: make(map[string]struct{}),
	}
}

// Crawl begins crawling from the given URL and returns a channel of results.
// The channel is closed when the crawl finishes or the context is cancelled.
func (c *Crawler) Crawl(ctx context.Context, start string) <-chan Result {
	results := make(chan Result)
	tasks := make(chan task)

	var taskWG sync.WaitGroup
	var workerWG sync.WaitGroup

	// Seed the initial URL.
	taskWG.Add(1)
	go func() {
		defer taskWG.Done()
		c.enqueue(ctx, tasks, task{URL: start, Depth: 0})
	}()

	// Close the tasks channel when no tasks remain.
	go func() {
		taskWG.Wait()
		close(tasks)
	}()

	// Start worker goroutines.
	for i := 0; i < c.config.MaxWorkers; i++ {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			for t := range tasks {
				if ctx.Err() != nil {
					return
				}
				c.handleTask(ctx, t, &taskWG, tasks, results)
			}
		}()
	}

	// Close results when all workers finish.
	go func() {
		workerWG.Wait()
		close(results)
	}()

	return results
}

type task struct {
	URL   string
	Depth int
}

func (c *Crawler) handleTask(ctx context.Context, t task, taskWG *sync.WaitGroup, tasks chan<- task, results chan<- Result) {
	if t.Depth > c.config.MaxDepth {
		return
	}

	normalized, err := c.normalizeURL(t.URL)
	if err != nil {
		results <- Result{URL: t.URL, Error: fmt.Sprintf("invalid url: %v", err)}
		return
	}

	if !c.claimVisit(normalized) {
		return
	}

	parentURL, err := url.Parse(normalized)
	if err != nil {
		results <- Result{URL: t.URL, Error: fmt.Sprintf("invalid url: %v", err)}
		return
	}

	title, links, fetchErr := c.fetchPage(ctx, normalized)
	res := Result{URL: normalized, Title: title, Links: links}
	if fetchErr != nil {
		res.Error = fetchErr.Error()
	}
	results <- res

	if fetchErr != nil || t.Depth == c.config.MaxDepth {
		return
	}

	for _, link := range links {
		normalizedLink, err := c.normalizeURL(link)
		if err != nil {
			continue
		}

		if c.config.SameHost && !hostsMatch(parentURL, normalizedLink) {
			continue
		}

		if !c.claimVisit(normalizedLink) {
			continue
		}

		select {
		case <-ctx.Done():
			return
		default:
			taskWG.Add(1)
			tasks <- task{URL: normalizedLink, Depth: t.Depth + 1}
		}
	}
}

func (c *Crawler) fetchPage(ctx context.Context, pageURL string) (string, []string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}

	title, links := parseHTML(body, pageURL)
	return title, links, nil
}

func (c *Crawler) normalizeURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	if parsed.Host == "" {
		return "", errors.New("missing host")
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func parseHTML(body []byte, base string) (string, []string) {
	content := string(body)

	title := extractTitle(content)
	links := extractLinks(content, base)

	return strings.TrimSpace(title), links
}

func extractTitle(content string) string {
	titleRe := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`) //nolint:lll
	matches := titleRe.FindStringSubmatch(content)
	if len(matches) >= 2 {
		return strings.TrimSpace(htmlUnescape(matches[1]))
	}
	return ""
}

func extractLinks(content, base string) []string {
	// Match common variations of href attributes in anchor tags.
	linkRe := regexp.MustCompile(`(?is)<a[^>]*?href\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`) //nolint:lll
	rawLinks := linkRe.FindAllStringSubmatch(content, -1)

	links := make([]string, 0, len(rawLinks))
	for _, m := range rawLinks {
		href := strings.TrimSpace(m[1])
		href = strings.Trim(href, "\"'")
		if resolved := resolveURL(base, htmlUnescape(href)); resolved != "" {
			links = append(links, resolved)
		}
	}
	return links
}

func htmlUnescape(value string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
	)
	return replacer.Replace(value)
}

func resolveURL(base, href string) string {
	if href == "" || strings.HasPrefix(href, "#") {
		return ""
	}

	baseURL, err := url.Parse(base)
	if err != nil {
		return ""
	}

	resolved, err := baseURL.Parse(href)
	if err != nil {
		return ""
	}

	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}

	resolved.Fragment = ""
	return resolved.String()
}

func (c *Crawler) enqueue(ctx context.Context, tasks chan<- task, t task) {
	select {
	case <-ctx.Done():
		return
	default:
		tasks <- t
	}
}

func (c *Crawler) claimVisit(url string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.visited[url]; exists {
		return false
	}

	c.visited[url] = struct{}{}
	return true
}

func hostsMatch(parent *url.URL, child string) bool {
	childURL, err := url.Parse(child)
	if err != nil {
		return false
	}

	return strings.EqualFold(parent.Hostname(), childURL.Hostname())
}
