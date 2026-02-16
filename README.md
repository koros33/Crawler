## 📚 Learning Journey

This project was built as a learning exercise to understand:

### Core Concepts Implemented

#### 1. **Concurrency & Goroutines**
- Worker pool pattern with 5 concurrent scrapers
- Channel-based communication between discovery and scraping phases
- Mutex locks for shared state (visited URLs map)
- WaitGroups for graceful shutdown

```go
// Worker pool pattern
for i := 0; i < numWorkers; i++ {
    wg.Add(1)
    go worker(worklist, parser, db, &wg)
}
```

**What I learned**: How Go's goroutines enable concurrent I/O operations without the complexity of threads. The challenge was coordinating shutdown - ensuring all workers finish before the program exits.

#### 2. **HTML Parsing & Web Scraping**
- Using `golang.org/x/net/html` tokenizer for efficient parsing
- Extracting SEO elements (title, H1, meta tags)
- Link extraction and URL normalization

**What I learned**: The difference between parsing HTML as a token stream vs DOM tree. Token-based parsing is faster but requires state tracking.

#### 3. **Database Design with GORM**
- Schema design for crawl data
- Handling duplicate URLs with `uniqueIndex`
- Tracking crawl statistics over time

```go
// Prevent duplicate crawls
result := db.Where(Page{URL: data.URL}).FirstOrCreate(&page)
```

**What I learned**: The importance of database constraints and how ORMs abstract SQL while still requiring understanding of indexes and constraints.

#### 4. **HTTP Client Best Practices**
- Context-based timeouts to prevent hanging requests
- User-Agent rotation to avoid detection
- Proper response body closing to prevent memory leaks

**What I learned**: The subtle but critical importance of `defer resp.Body.Close()` - without it, connections leak and the program crashes under load.

#### 5. **Error Handling & Observability**
- Structured logging with `slog`
- Atomic counters for statistics
- Graceful degradation when pages fail

```go
var (
    successPages atomic.Int64
    failedPages  atomic.Int64
)
```

**What I learned**: In concurrent systems, regular variables aren't safe for counting. Atomic operations prevent race conditions without heavy locking.

---

## 🎯 Challenges Overcome

### Challenge 1: Race Conditions in URL Discovery
**Problem**: Multiple goroutines were adding the same URL to the visited map, causing duplicates.

**Solution**: Added mutex locks around the visited map:
```go
mu.Lock()
if visited[url] {
    mu.Unlock()
    return
}
visited[url] = true
mu.Unlock()
```

**Lesson**: Shared mutable state in concurrent programs requires synchronization.

---

### Challenge 2: Graceful Shutdown
**Problem**: Program was exiting before all URLs were scraped.

**Solution**: Used a `done` channel to signal when discovery completes, then closed the worklist:
```go
<-done              // Wait for discovery
close(worklist)     // Signal no more URLs
wg.Wait()          // Wait for workers to drain
```

**Lesson**: Channels aren't just for data - they're powerful signaling primitives.

---

### Challenge 3: Memory Leaks from HTTP Responses
**Problem**: Program memory grew unbounded during long crawls.

**Solution**: Ensured every `http.Response.Body` is closed:
```go
resp, err := makeRequest(url)
if err != nil {
    return err
}
defer resp.Body.Close()  // Critical!
```

**Lesson**: Go's garbage collector can't clean up network resources - you must explicitly close them.

---

### Challenge 4: CGO and SQLite Dependencies (The Big One!)
**Problem**: Windows compilation failed with `cgo: C compiler "gcc" not found`. The default GORM SQLite driver (`mattn/go-sqlite3`) requires CGO, which needs a C compiler.

**The Journey**:
1. Initially tried enabling CGO → needs MinGW/gcc on Windows
2. Attempted to use `modernc.org/sqlite` (pure Go) but kept getting pulled back to CGO driver
3. `go mod tidy` kept re-adding `mattn/go-sqlite3` as a transitive dependency
4. Even after manual removal, it would reappear

**Solution**: Switched to `github.com/glebarez/sqlite` - a GORM driver that exclusively uses pure Go SQLite:
```go
import sqlite "github.com/glebarez/sqlite"
```

**What went wrong**:
- GORM's default `gorm.io/driver/sqlite` supports BOTH CGO and pure Go drivers
- Go's module system auto-selected the CGO version
- Windows lacks gcc by default, causing compilation to fail

**Lesson**: Dependency management matters! Some packages have platform-specific requirements. Pure Go alternatives exist for cross-platform compatibility. When fighting with dependencies:
- Check `go.mod` for unwanted transitive deps
- Use `go mod edit -droprequire` to force removal
- Consider alternative packages that avoid CGO entirely

---

## 🔬 What I'd Improve Next

1. **Respect robots.txt**
   - Parse and honor crawl rules
   - Implement per-domain rate limiting

2. **Better Error Recovery**
   - Retry failed requests with exponential backoff
   - Separate temporary vs permanent failures

3. **Observability**
   - Real-time progress dashboard
   - Live crawl speed metrics (pages/sec)
   - Structured JSON logs for analysis

4. **Testing**
   - Unit tests for parser
   - Integration tests with mock HTTP server
   - Benchmark tests for concurrency limits

5. **Distribution**
   - Redis-backed worklist for multi-machine crawling
   - Distributed database (PostgreSQL)
   - Kubernetes deployment

---

## 💡 Key Takeaways

### Technical Skills Gained
- ✅ Go concurrency patterns (goroutines, channels, mutexes)
- ✅ HTTP client programming and network I/O
- ✅ HTML parsing and DOM traversal
- ✅ Database design and ORM usage
- ✅ Error handling in distributed systems
- ✅ Resource management (connections, file descriptors)
- ✅ **Dependency management and CGO cross-compilation issues**

### Software Engineering Practices
- ✅ Code organization and modularity
- ✅ Configuration management
- ✅ Documentation (this README!)
- ✅ Debugging concurrent systems
- ✅ Performance profiling and optimization
- ✅ **Troubleshooting build toolchain problems**

### What I'd Do Differently
- Start with tests from day 1 (TDD approach)
- Use interfaces earlier for better mocking
- Research platform compatibility before choosing dependencies
- Version the database schema with migrations
- **Check for pure Go alternatives to CGO dependencies upfront**

---

## 📊 Performance Results

**Test Crawl** (http://books.toscrape.com):
- **Pages crawled**: 100
- **Success rate**: 100% (no failures)
- **Total time**: 35.8 seconds
- **Average speed**: ~2.8 pages/second
- **Concurrent workers**: 5

---

## 🐛 Troubleshooting

**"CGO_ENABLED=0, go-sqlite3 requires cgo" error on Windows:**

This means you're using a SQLite driver that requires a C compiler. Switch to pure Go:

```bash
# Remove CGO-based driver
go get gorm.io/driver/sqlite@none

# Install pure Go alternative
go get github.com/glebarez/sqlite
```

Update imports:
```go
import sqlite "github.com/glebarez/sqlite"  // Instead of gorm.io/driver/sqlite
```

**Database locked / file in use:**

Only one process can write to SQLite at a time. Stop any running crawler instances before starting a new one:
```bash
# Linux/Mac
killall main

# Windows
taskkill /F /IM go.exe
```

To prevent conflicts, use unique database names per crawl:
```go
dbName := fmt.Sprintf("crawler_%s.db", time.Now().Format("20060102_150405"))
```

---

## 📖 Resources That Helped

### Books
- *Concurrency in Go* by Katherine Cox-Buday
- *The Go Programming Language* by Donovan & Kernighan

### Articles
- [Go's Concurrency Patterns](https://go.dev/blog/pipelines)
- [Effective Go](https://go.dev/doc/effective_go)
- [CGO is Not Go](https://dave.cheney.net/2016/01/18/cgo-is-not-go) - Understanding CGO tradeoffs

### Practice Sites
- [books.toscrape.com](http://books.toscrape.com) - My main testing ground
- [quotes.toscrape.com](https://quotes.toscrape.com) - Simple structure for debugging

---

## 🎓 About This Project

Built as part of my journey to learn:
- Backend development with Go
- Concurrent programming
- Web scraping & data extraction
- Database design

**Timeline**: ~2 weeks from concept to working prototype (including 1 day fighting with CGO )

**Why Go?** I chose Go for this project because:
1. Excellent concurrency primitives (goroutines are lightweight)
2. Fast compilation and execution
3. Strong standard library for HTTP and HTML
4. Growing adoption in backend/infrastructure roles

**Next Project**: Building a Google SERP scraper to extract search results and competitor analysis data.

---

## 🤝 For Recruiters & Hiring Managers

This project demonstrates:

✅ **Problem Solving**: Broke down web crawling into discoverable components  
✅ **Go Proficiency**: Leveraged goroutines, channels, and standard library effectively  
✅ **System Design**: Designed a pipeline architecture (discovery → scraping → storage)  
✅ **Best Practices**: Proper error handling, resource cleanup, and data persistence  
✅ **Self-Learning**: Researched and implemented unfamiliar concepts (HTML parsing, concurrency patterns)  
✅ **Documentation**: Clear README with architecture diagrams and usage examples  
✅ **Persistence**: Debugged complex toolchain issues (CGO/dependency conflicts) without giving up  
