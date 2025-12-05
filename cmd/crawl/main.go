package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"clonewogg/internal/crawler"
)

func main() {
	var (
		startURL  string
		depth     int
		workers   int
		sameHost  bool
		timeoutMs int
		jsonOut   bool
	)

	flag.StringVar(&startURL, "url", "", "Starting URL to crawl")
	flag.IntVar(&depth, "depth", 1, "Maximum crawl depth")
	flag.IntVar(&workers, "workers", 4, "Number of concurrent workers")
	flag.BoolVar(&sameHost, "same-host", true, "Restrict crawl to the starting host")
	flag.IntVar(&timeoutMs, "timeout", 15000, "Request timeout in milliseconds")
	flag.BoolVar(&jsonOut, "json", false, "Output results as JSON lines")
	flag.Parse()

	if startURL == "" {
		log.Fatal("missing required -url flag")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := crawler.Config{
		MaxDepth:   depth,
		MaxWorkers: workers,
		SameHost:   sameHost,
		Timeout:    time.Duration(timeoutMs) * time.Millisecond,
	}

	c := crawler.New(cfg)
	results := c.Crawl(ctx, startURL)

	encoder := json.NewEncoder(os.Stdout)

	for res := range results {
		if jsonOut {
			if err := encoder.Encode(res); err != nil {
				log.Printf("failed to write json: %v", err)
			}
			continue
		}

		fmt.Printf("URL: %s\n", res.URL)
		if res.Title != "" {
			fmt.Printf("Title: %s\n", res.Title)
		}
		if res.Error != "" {
			fmt.Printf("Error: %s\n", res.Error)
		}
		if len(res.Links) > 0 {
			fmt.Println("Links:")
			for _, link := range res.Links {
				fmt.Printf("  - %s\n", link)
			}
		}
		fmt.Println()
	}
}
