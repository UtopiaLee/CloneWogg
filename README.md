# CloneWogg Crawler

A minimal Go project for crawling web pages from a starting URL. The crawler fetches pages concurrently, extracts page titles and links, and optionally restricts traversal to the starting host.

## Features
- Concurrent crawling with configurable worker count
- Depth-limited traversal to control how far the crawl goes
- Optional same-host restriction to avoid leaving the starting domain
- Graceful shutdown on `SIGINT`/`SIGTERM`
- Plain text or JSON line output

## Getting Started

### Prerequisites
- Go 1.21 or newer

### Running

```bash
go run ./cmd/crawl -url https://example.com -depth 1 -workers 4 -same-host=true
```

Flags:

- `-url` (required): Starting URL to crawl.
- `-depth`: Maximum crawl depth (default `1`).
- `-workers`: Number of concurrent workers (default `4`).
- `-same-host`: Restrict crawl to the starting host (default `true`).
- `-timeout`: Request timeout in milliseconds (default `15000`).
- `-json`: Output results as JSON lines.

Example with JSON output:

```bash
go run ./cmd/crawl -url https://example.com -depth 0 -json
```

This will fetch only the starting page and emit a single JSON object describing the result.
