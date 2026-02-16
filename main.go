package main

import (
    "context"
    "fmt"
    "io"
    "log"
    "log/slog"
    "math/rand"
    "net/http"
    "net/url"
    "sync"
    "sync/atomic"
    "time"

    "golang.org/x/net/html"
    "github.com/glebarez/sqlite"//love you bro
    "gorm.io/gorm"
    "gorm.io/gorm/logger"
    //_ "modernc.org/sqlite"  
)

// ============================================================================
// DATABASE MODELS
// ============================================================================

type Page struct {
	ID              uint      `gorm:"primaryKey"`
	URL             string    `gorm:"uniqueIndex;not null"`
	Title           string    `gorm:"size:500"`
	H1              string    `gorm:"size:500"`
	MetaDescription string    `gorm:"size:1000"`
	StatusCode      int       `gorm:"index"`
	CrawledAt       time.Time `gorm:"index"`
	CreatedAt       time.Time
}

type CrawlStats struct {
	ID           uint `gorm:"primaryKey"`
	TotalPages   int
	SuccessPages int
	FailedPages  int
	Duration     int64 // seconds
	StartURL     string
	CrawledAt    time.Time
}

// ============================================================================
// SEO DATA & PARSER
// ============================================================================

type SEOData struct {
	URL             string
	Title           string
	H1              string
	MetaDescription string
	StatusCode      int
}

type Parser interface {
	GetSEOData(resp *http.Response) (SEOData, error)
}

type DefaultParser struct{}

func (p *DefaultParser) GetSEOData(resp *http.Response) (SEOData, error) {
	data := SEOData{
		URL:        resp.Request.URL.String(),
		StatusCode: resp.StatusCode,
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return data, err
	}

	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				if n.FirstChild != nil {
					data.Title = n.FirstChild.Data
				}
			case "h1":
				if n.FirstChild != nil && data.H1 == "" {
					data.H1 = n.FirstChild.Data
				}
			case "meta":
				var name, content string
				for _, attr := range n.Attr {
					if attr.Key == "name" {
						name = attr.Val
					}
					if attr.Key == "content" {
						content = attr.Val
					}
				}
				if name == "description" {
					data.MetaDescription = content
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(doc)

	return data, nil
}

// ============================================================================
// GLOBAL VARIABLES
// ============================================================================

var (
	completedPages atomic.Int64
	successPages   atomic.Int64
	failedPages    atomic.Int64
	totalPages     int
)

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
}

// ============================================================================
// DATABASE FUNCTIONS
// ============================================================================

func initDB() (*gorm.DB, error) {
	 dbName := fmt.Sprintf("crawler_%s.db", time.Now().Format("20060102_150405"))
    
    db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{
        Logger: logger.Default.LogMode(logger.Silent),
    })

	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	err = db.AutoMigrate(&Page{}, &CrawlStats{})
	if err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return db, nil
}

func savePage(db *gorm.DB, data SEOData) error {
	page := Page{
		URL:             data.URL,
		Title:           data.Title,
		H1:              data.H1,
		MetaDescription: data.MetaDescription,
		StatusCode:      data.StatusCode,
		CrawledAt:       time.Now(),
	}

	result := db.Where(Page{URL: data.URL}).FirstOrCreate(&page)
	return result.Error
}

func saveCrawlStats(db *gorm.DB, startURL string, duration time.Duration, total, success, failed int) error {
	stats := CrawlStats{
		TotalPages:   total,
		SuccessPages: success,
		FailedPages:  failed,
		Duration:     int64(duration.Seconds()),
		StartURL:     startURL,
		CrawledAt:    time.Now(),
	}

	return db.Create(&stats).Error
}

// ============================================================================
// HTTP REQUEST
// ============================================================================

func randomUserAgent() string {
	return userAgents[rand.Intn(len(userAgents))]
}

func makeRequest(url string) (*http.Response, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		slog.Error("failed to create request", "error", err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", randomUserAgent())

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

func makeRequestWithContext(ctx context.Context, url string) (*http.Response, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", randomUserAgent())

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

// ============================================================================
// URL EXTRACTION
// ============================================================================

func discoverURLs(seedURL string, worklist chan<- string, maxURLs int, done chan<- bool) {
	visited := make(map[string]bool)
	var mu sync.Mutex
	count := 0

	var crawl func(string)
	crawl = func(url string) {
		mu.Lock()
		if visited[url] || count >= maxURLs {
			mu.Unlock()
			return
		}
		visited[url] = true
		count++
		current := count
		mu.Unlock()

		resp, err := makeRequest(url)
		if err != nil {
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return
		}

		worklist <- url // Add to worklist for scraping

		links := extractLinks(resp.Body, url)
		for _, link := range links {
			if current < maxURLs {
				go crawl(link)
			}
		}
	}

	go func() {
		crawl(seedURL)
		time.Sleep(5 * time.Second) // Wait for goroutines to finish
		done <- true
	}()
}

func extractLinks(body io.Reader, baseURL string) []string {
	var links []string
	base, _ := url.Parse(baseURL)

	tokenizer := html.NewTokenizer(body)
	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}

		token := tokenizer.Token()
		if tt == html.StartTagToken && token.Data == "a" {
			for _, attr := range token.Attr {
				if attr.Key == "href" {
					link, err := base.Parse(attr.Val)
					if err == nil {
						links = append(links, link.String())
					}
				}
			}
		}
	}
	return links
}

// ============================================================================
// SCRAPING
// ============================================================================

func scrapeURLFromWorklist(url string, parser Parser, db *gorm.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := makeRequestWithContext(ctx, url)
	if err != nil {
		failedPages.Add(1)
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := parser.GetSEOData(resp)
	if err != nil {
		failedPages.Add(1)
		return fmt.Errorf("parse failed: %w", err)
	}

	if err := savePage(db, data); err != nil {
		failedPages.Add(1)
		return fmt.Errorf("db insert failed: %w", err)
	}

	successPages.Add(1)
	completedPages.Add(1)
	return nil
}

func worker(worklist <-chan string, parser Parser, db *gorm.DB, wg *sync.WaitGroup) {
	defer wg.Done()
	for url := range worklist {
		if err := scrapeURLFromWorklist(url, parser, db); err != nil {
			log.Printf("failed to scrape %s: %v", url, err)
		}
	}
}

// ============================================================================
// MAIN
// ============================================================================

func main() {
	rand.Seed(time.Now().UnixNano())
	startTime := time.Now()

	// Initialize DB
	db, err := initDB()
	if err != nil {
		log.Fatal("failed to connect database:", err)
	}

	// Setup parser
	parser := &DefaultParser{}

	// Setup worklist channel
	worklist := make(chan string, 100)
	done := make(chan bool)

	// Start workers
	var wg sync.WaitGroup
	numWorkers := 5

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(worklist, parser, db, &wg)
	}

	// Discover & feed URLs
	seedURL := "http://books.toscrape.com"
	maxURLs := 100

	go discoverURLs(seedURL, worklist, maxURLs, done)

	// Wait for discovery to finish
	<-done
	close(worklist)
	wg.Wait()

	// Save stats
	duration := time.Since(startTime)
	saveCrawlStats(db, seedURL, duration, int(completedPages.Load()), 
		int(successPages.Load()), int(failedPages.Load()))

	log.Printf("Scraping complete! Success: %d, Failed: %d, Duration: %v",
		successPages.Load(), failedPages.Load(), duration)
}