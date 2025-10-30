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
	Depth      int
}

type CrawlJob struct {
	URL   string
	Depth int
}

var visited = struct {
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
	jobs := make(chan CrawlJob)
	results := make(chan LinkStatus)

	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < *workers; i++ {
		go worker(jobs, results)
	}

	manager(&wg, jobs, results, *startURLStr, *depth)

	wg.Wait()
	close(jobs)
	close(results)
	fmt.Println("Crawl completed successfully.")
}

func manager(
	wg *sync.WaitGroup,
	jobs chan<- CrawlJob,
	results <-chan LinkStatus,
	startURL string,
	maxDepth int,
) {
	pendingJobs := make(map[string]struct{})
	var mu sync.Mutex

	mu.Lock()
	pendingJobs[startURL] = struct{}{}
	mu.Unlock()
	wg.Add(1)
	jobs <- CrawlJob{URL: startURL, Depth: 0}

	go func() {
		for result := range results {
			printLinkStatus(result)

			mu.Lock()
			delete(pendingJobs, result.URL)
			mu.Unlock()

			if result.Error == nil &&
				result.StatusCode >= 200 &&
				result.StatusCode < 300 &&
				result.StatusCode != http.StatusNoContent &&
				result.Depth < maxDepth {
				newLinks := extractLinks(
					http.DefaultClient,
					result.URL,
					strings.Split(result.URL, "://")[0]+"://"+strings.Split(result.URL, "/")[2],
				)

				for _, link := range newLinks {
					link = strings.TrimSuffix(link, "/")
					visited.Lock()
					_, seen := visited.urls[link]
					visited.Unlock()

					if !seen {
						mu.Lock()
						if _, isPending := pendingJobs[link]; !isPending {
							pendingJobs[link] = struct{}{}
							wg.Add(1)
							go func() { jobs <- CrawlJob{URL: link, Depth: result.Depth + 1} }()
						}
						mu.Unlock()
					}
				}
			}
			wg.Done()
		}
	}()
}

// worker fetches a URL, extracts links, and sends results/new URLs
func worker(jobs <-chan CrawlJob, results chan<- LinkStatus) {
	client := &http.Client{Timeout: 10 * time.Second} // HTTP client with a timeout

	for job := range jobs {
		visited.Lock()
		visited.urls[job.URL] = struct{}{}
		visited.Unlock()

		status := checkURL(client, job.URL, job.Depth)

		results <- status
	}
}

func checkURL(client *http.Client, urlStr string, depth int) LinkStatus {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return LinkStatus{URL: urlStr, Error: fmt.Errorf("failed to create request: %w", err), Depth: depth}
	}
	req.Header.Set("User-Agent", "Go-Web-Crawler/1.0 (https://github.com/adnanahmady/go-web-crawler)")

	res, err := client.Do(req)
	if err != nil {
		return LinkStatus{URL: urlStr, Error: fmt.Errorf("failed to fetch URL: %w", err), Depth: depth}
	}
	defer res.Body.Close()

	return LinkStatus{URL: urlStr, StatusCode: res.StatusCode, Depth: depth}
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
	prefix := fmt.Sprintf("[%d] ", status.Depth)
	if status.Error != nil {
		log.Printf("%sError for %s: %v\n", prefix, status.URL, status.Error)
	} else {
		log.Printf("%sStatus for %s: %d\n", prefix, status.URL, status.StatusCode)
	}
}
