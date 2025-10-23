package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// LinkStatus represents the status of a link
type LinkStatus struct {
	URL        string
	StatusCode int
	Error      error
}

var visitedURLs = struct {
	sync.Mutex
	urls map[string]struct{}
}{
	urls: make(map[string]struct{}),
}

func main() {
	startURLStr := flag.String("url", "", "Starting URL to crawl")
	depth := flag.Int("depth", 1, "Maximum depth to crawl (0 for current page only, 1 for current + first level links, etc)")
	workers := flag.Int("workers", 5, "Number of concurrent workers (goroutines)")
	flag.Parse()

	if *startURLStr == "" {
		fmt.Println("Error: please provide a starting URL using the --url flag.")
		flag.Usage()
		os.Exit(1)
	}

	// Parse the starting URL to get its base domain for internal link filtering
	parsedURL, err := url.Parse(*startURLStr)
	if err != nil {
		log.Fatalf("Error parsing starting URL: %v", err)
	}
	baseURL := parsedURL.Scheme + "://" + parsedURL.Host

	fmt.Printf("Starting crawl from: %s (Depth: %d, Workers: %d)\n", *startURLStr, *depth, *workers)
	fmt.Printf("Base domain for internal links: %s\n", baseURL)

	// Channels
	// 'toProcess' sends URLs to worker goroutines
	toProcess := make(chan string)
	// 'results' sends LinkStatus results back from workers
	results := make(chan LinkStatus)
	// 'done' signals when a worker has finished its current URL
	workerDone := make(chan struct{})

	// Start worker goroutines
	for i := 0; i < *workers; i++ {
		go worker(toProcess, results, workerDone, baseURL)
	}

	go func() {
		toProcess <- *startURLStr
	}()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		activeCrawls := 0
		for {
			select {
			case status := <-results:
				printLinkStatus(status)
				activeCrawls--
				if activeCrawls == 0 && *depth > 0 {
					wg.Done()
				}
			case <-workerDone:
			case <-time.After(5 * time.Second):
				if activeCrawls == 0 && len(toProcess) == 0 {
					fmt.Println("No more URLs to process. Shutting down.")
					close(toProcess)
					return
				}
			}
			if activeCrawls == 0 && len(toProcess) == 0 {
				time.Sleep(100 * time.Millisecond)
				if activeCrawls == 0 && len(toProcess) == 0 {
					fmt.Println("No more URLs to process. Shutting down.")
					close(toProcess)
					return
				}
			}
		}
	}()

	wg.Wait()
	fmt.Println("Crawl completed successfully.")
}

// worker fetches a URL, extracts links, and sends results/new URLs
func worker(toProcess chan string, results chan<- LinkStatus, workerDone chan<- struct{}, baseURL string) {
	client := &http.Client{Timeout: 10 * time.Second} // HTTP client with a timeout

	for urlStr := range toProcess {
		func() {
			defer func() {
				workerDone <- struct{}{} // Signal that this worker is done with current URL
			}()

			visitedURLs.Lock()
			if _, seen := visitedURLs.urls[urlStr]; seen {
				visitedURLs.Unlock()
				return // Already visited
			}
			visitedURLs.urls[urlStr] = struct{}{}
			visitedURLs.Unlock()

			status := checkURL(client, urlStr)
			results <- status // Send result back

			if status.Error == nil && status.StatusCode >= 200 && status.StatusCode < 300 {
				newLinks := extractLinks(client, urlStr, baseURL)
				for _, link := range newLinks {
					toProcess <- link
				}
			}
		}()
	}
}

func checkURL(client *http.Client, urlStr string) LinkStatus {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return LinkStatus{URL: urlStr, Error: fmt.Errorf("failed to create request: %w", err)}
	}
	req.Header.Set("User-Agent", "Go-Web-Crawler/1.0 (https://github.com/adnanahmady/go-web-crawler)")

	res, err := client.Do(req)
	if err != nil {
		return LinkStatus{URL: urlStr, Error: fmt.Errorf("failed to fetch URL: %w", err)}
	}
	defer res.Body.Close()

	return LinkStatus{URL: urlStr, StatusCode: res.StatusCode}
}

func extractLinks(client *http.Client, parentURL string, baseURL string) []string {
	req, err := http.NewRequest("GET", parentURL, nil)
	if err != nil {
		log.Printf("Error creating request for %s: %v", parentURL, err)
		return nil
	}
	req.Header.Set("User-Agent", "Go-Web-Crawler/1.0 (https://github.com/adnanahmady/go-web-crawler)")

	res, err := client.Do(req)
	if err != nil {
		log.Printf("Error fetching %s: %v", parentURL, err)
		return nil
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		log.Printf("Non-OK status for %s: %d %s", parentURL, res.StatusCode, res.Status)
		return nil
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Printf("Error parsing HTML form %s: %v", parentURL, err)
		return nil
	}

	var links []string
	doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists {
			return
		}

		resolvedURL, err := url.Parse(href)
		if err != nil {
			return
		}
		base, _ := url.Parse(parentURL)
		absoluteURL := base.ResolveReference(resolvedURL).String()

		if strings.HasPrefix(absoluteURL, baseURL) {
			links = append(links, absoluteURL)
		}
	})
	return links
}

func printLinkStatus(status LinkStatus) {
	if status.Error != nil {
		log.Printf("Error for %s: %v\n", status.URL, status.Error)
	} else {
		log.Printf("Status for %s: %d\n", status.URL, status.StatusCode)
	}
}
