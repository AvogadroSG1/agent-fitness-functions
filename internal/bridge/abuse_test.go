package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/poconnor/calm-poc/internal/analyzer"
	"github.com/poconnor/calm-poc/internal/calm"
	"github.com/poconnor/calm-poc/internal/fitness"
)

// --- body size cap ---

func TestHandlerCheckRejectsOversizedRequestBodyWith413(t *testing.T) {
	server := httptest.NewServer(NewHandler(nil, nil))
	defer server.Close()

	body := bytes.Repeat([]byte("x"), 5<<20+1)
	response, err := http.Post(server.URL+"/check", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 for body exceeding 5 MB", response.StatusCode)
	}
}

func TestHandlerCheckAcceptsBodyBelowSizeCap(t *testing.T) {
	repo := "repo-one"
	store := newTestConfigStore(t)
	writeRepoConfig(t, store, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithOptions(Checker{
		ConfigStore: store,
		PatternPath: writeTestPattern(t),
		Analyzers:   map[string]SourceAnalyzer{"go": fakeGoAnalyzer(analyzer.AnalysisResult{Language: "go"})},
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: true, Output: `{"hasErrors":false}`}, nil
		}),
	}, nil, HandlerOptions{}))
	defer server.Close()

	content := strings.Repeat("a", 100)
	body := `{"repo":` + jsonString(repo) + `,"file":"p.go","language":"go","proposed_content":` + jsonString(content) + `}`
	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d body = %q, want 200", response.StatusCode, b)
	}
}

// --- per-client rate limiting ---

func TestHandlerCheckRateLimitsCallerWith429(t *testing.T) {
	store := newTestConfigStore(t)
	limiter := NewFixedWindowRateLimiter(2, time.Minute)
	server := httptest.NewServer(NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{
		RateLimiter: limiter,
	}))
	defer server.Close()

	for i := range 2 {
		if code := postMinimalCheck(t, server.URL); code == http.StatusTooManyRequests {
			t.Fatalf("request %d: got 429 before limit reached", i+1)
		}
	}
	if code := postMinimalCheck(t, server.URL); code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 after rate limit exceeded", code)
	}
}

func TestHandlerCheckRateLimitsPerCallerIndependently(t *testing.T) {
	store := newTestConfigStore(t)
	limiter := NewFixedWindowRateLimiter(1, time.Minute)
	handler := NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RateLimiter: limiter})

	callerAReq := syntheticValidationRequest(t, "caller-a")
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, callerAReq)
	if rw.Code == http.StatusTooManyRequests {
		t.Fatalf("caller-a first request should not be rate-limited")
	}

	rw = httptest.NewRecorder()
	handler.ServeHTTP(rw, syntheticValidationRequest(t, "caller-a"))
	if rw.Code != http.StatusTooManyRequests {
		t.Fatalf("caller-a second request: status = %d, want 429", rw.Code)
	}

	rw = httptest.NewRecorder()
	handler.ServeHTTP(rw, syntheticValidationRequest(t, "caller-b"))
	if rw.Code == http.StatusTooManyRequests {
		t.Fatalf("caller-b should not be rate-limited by caller-a, got 429")
	}
}

func TestHandlerCheckRateLimiterWindowResets(t *testing.T) {
	store := newTestConfigStore(t)
	now := time.Now()
	fl := newTestFixedWindowRateLimiter(1, time.Minute, now)
	server := httptest.NewServer(NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RateLimiter: fl}))
	defer server.Close()

	postMinimalCheck(t, server.URL)
	if code := postMinimalCheck(t, server.URL); code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 while in window", code)
	}

	fl.setNow(now.Add(time.Minute + time.Second))
	if code := postMinimalCheck(t, server.URL); code == http.StatusTooManyRequests {
		t.Fatalf("got 429 after window reset, want request allowed")
	}
}

// --- per-repo concurrency cap ---

func TestHandlerCheckAllowsTenConcurrentAnalyses(t *testing.T) {
	repo := "repo-one"
	store := newTestConfigStore(t)
	writeRepoConfig(t, store, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})

	release := make(chan struct{})
	var inflight atomic.Int32
	server := httptest.NewServer(NewHandlerWithOptions(Checker{
		ConfigStore: store,
		PatternPath: writeTestPattern(t),
		Analyzers:   map[string]SourceAnalyzer{"go": blockingAnalyzer(release, &inflight)},
		Validator:   alwaysPassValidator(),
	}, nil, HandlerOptions{MaxConcurrentAnalysesPerRepo: 10}))
	defer server.Close()

	codes := launchConcurrentChecks(t, server.URL, repo, 10)
	waitForInflight(t, &inflight, 10)
	close(release)
	assertAllStatus(t, codes, 10, http.StatusOK)
}

func TestHandlerCheckRejects11thConcurrentAnalysisAsBusy(t *testing.T) {
	repo := "repo-one"
	store := newTestConfigStore(t)
	writeRepoConfig(t, store, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})

	release := make(chan struct{})
	reached10 := make(chan struct{})
	server := httptest.NewServer(NewHandlerWithOptions(Checker{
		ConfigStore: store,
		PatternPath: writeTestPattern(t),
		Analyzers:   map[string]SourceAnalyzer{"go": blockingAnalyzerNotify(release, reached10)},
		Validator:   alwaysPassValidator(),
	}, nil, HandlerOptions{MaxConcurrentAnalysesPerRepo: 10}))
	defer server.Close()

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			resp, err := http.Post(server.URL+"/check", "application/json",
				strings.NewReader(checkBodyForFile(repo, fileN(n))))
			if err == nil {
				resp.Body.Close()
			}
		}(i)
	}
	waitForChan(t, reached10, 2*time.Second, "10 analyses inflight")

	resp, err := http.Post(server.URL+"/check", "application/json",
		strings.NewReader(checkBodyForFile(repo, "f10.go")))
	if err != nil {
		t.Fatalf("11th POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("11th request: status = %d, want 503", resp.StatusCode)
	}

	close(release)
	wg.Wait()
}

// --- analyzer timeout ---

func TestHandlerCheckAnalyzerTimeoutReturns504(t *testing.T) {
	repo := "repo-one"
	store := newTestConfigStore(t)
	writeRepoConfig(t, store, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})

	server := httptest.NewServer(NewHandlerWithOptions(Checker{
		ConfigStore:     store,
		PatternPath:     writeTestPattern(t),
		Analyzers:       map[string]SourceAnalyzer{"go": hangingAnalyzer()},
		AnalyzerTimeout: 50 * time.Millisecond,
	}, nil, HandlerOptions{}))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json",
		strings.NewReader(checkBodyForFile(repo, "f.go")))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusGatewayTimeout {
		b, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d body = %q, want 504 on analyzer timeout", response.StatusCode, b)
	}
}

// --- enforcement-on-error routing ---

func TestHandlerCheckEnforcementOnErrorAdvisoryMapsAnalyzerCrashToAdvisory(t *testing.T) {
	assertEnforcementOnError(t, EnforcementOnErrorAdvisory, crashingAnalyzer(), http.StatusOK, fitness.StatusAdvisory)
}

func TestHandlerCheckEnforcementOnErrorPassMapsAnalyzerCrashToPass(t *testing.T) {
	assertEnforcementOnError(t, EnforcementOnErrorPass, crashingAnalyzer(), http.StatusOK, fitness.StatusPass)
}

func TestHandlerCheckEnforcementOnErrorBlockMapsAnalyzerCrashToServiceUnavailable(t *testing.T) {
	assertEnforcementOnError(t, EnforcementOnErrorBlock, crashingAnalyzer(), http.StatusServiceUnavailable, "")
}

func TestHandlerCheckEnforcementOnErrorAdvisoryMapsTimeoutToAdvisory(t *testing.T) {
	assertEnforcementOnError(t, EnforcementOnErrorAdvisory, hangingAnalyzer(), http.StatusOK, fitness.StatusAdvisory)
}

func TestHandlerCheckEnforcementOnErrorPassMapsTimeoutToPass(t *testing.T) {
	assertEnforcementOnError(t, EnforcementOnErrorPass, hangingAnalyzer(), http.StatusOK, fitness.StatusPass)
}

func assertEnforcementOnError(t *testing.T, errMode ErrorEnforcementMode, a SourceAnalyzer, wantStatus int, wantValidationStatus fitness.Status) {
	t.Helper()
	repo := "repo-one"
	store := newTestConfigStore(t)
	writeRepoConfigWithErrorMode(t, store, repo, EnforcementBlock, errMode, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithOptions(Checker{
		ConfigStore:     store,
		PatternPath:     writeTestPattern(t),
		Analyzers:       map[string]SourceAnalyzer{"go": a},
		AnalyzerTimeout: 50 * time.Millisecond,
	}, nil, HandlerOptions{}))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json",
		strings.NewReader(checkBodyForFile(repo, "f.go")))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		b, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d body = %q, want %d", response.StatusCode, b, wantStatus)
	}
	if wantValidationStatus == "" {
		return
	}
	var cr fitness.ValidationResult
	if err := json.NewDecoder(response.Body).Decode(&cr); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if cr.Status != wantValidationStatus {
		t.Fatalf("validation status = %q, want %q", cr.Status, wantValidationStatus)
	}
}

// --- test helpers ---

func postMinimalCheck(t *testing.T, serverURL string) int {
	t.Helper()
	resp, err := http.Post(serverURL+"/check", "application/json",
		strings.NewReader(`{"repo":"x","file":"f.go","language":"go","proposed_content":""}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func syntheticValidationRequest(t *testing.T, callerCN string) *http.Request {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, "/check",
		strings.NewReader(`{"repo":"x","file":"f.go","language":"go","proposed_content":""}`))
	req.Header.Set("Content-Type", "application/json")
	return req.WithContext(context.WithValue(req.Context(), callerContextKey{}, callerCN))
}

func blockingAnalyzer(release <-chan struct{}, inflight *atomic.Int32) SourceAnalyzer {
	return AnalyzerFunc(func(ctx context.Context, _ AnalysisRequest) (analyzer.AnalysisResult, error) {
		inflight.Add(1)
		defer inflight.Add(-1)
		select {
		case <-release:
		case <-ctx.Done():
			return analyzer.AnalysisResult{}, ctx.Err()
		}
		return analyzer.AnalysisResult{Language: "go"}, nil
	})
}

func blockingAnalyzerNotify(release <-chan struct{}, reached10 chan<- struct{}) SourceAnalyzer {
	var inflight atomic.Int32
	var once sync.Once
	return AnalyzerFunc(func(ctx context.Context, _ AnalysisRequest) (analyzer.AnalysisResult, error) {
		if inflight.Add(1) == 10 {
			once.Do(func() { close(reached10) })
		}
		defer inflight.Add(-1)
		select {
		case <-release:
		case <-ctx.Done():
			return analyzer.AnalysisResult{}, ctx.Err()
		}
		return analyzer.AnalysisResult{Language: "go"}, nil
	})
}

func hangingAnalyzer() SourceAnalyzer {
	return AnalyzerFunc(func(ctx context.Context, _ AnalysisRequest) (analyzer.AnalysisResult, error) {
		select {
		case <-ctx.Done():
			return analyzer.AnalysisResult{}, ctx.Err()
		case <-time.After(30 * time.Second):
			return analyzer.AnalysisResult{Language: "go"}, nil
		}
	})
}

func crashingAnalyzer() SourceAnalyzer {
	return AnalyzerFunc(func(context.Context, AnalysisRequest) (analyzer.AnalysisResult, error) {
		return analyzer.AnalysisResult{}, infrastructureError("analyzer crashed", nil)
	})
}

func alwaysPassValidator() Validator {
	return validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
		return calm.ValidationResult{Valid: true, Output: `{"hasErrors":false}`}, nil
	})
}

func launchConcurrentChecks(t *testing.T, serverURL, repo string, n int) chan int {
	t.Helper()
	codes := make(chan int, n)
	for i := range n {
		go func(idx int) {
			resp, err := http.Post(serverURL+"/check", "application/json",
				strings.NewReader(checkBodyForFile(repo, fileN(idx))))
			if err != nil {
				codes <- 0
				return
			}
			resp.Body.Close()
			codes <- resp.StatusCode
		}(i)
	}
	return codes
}

func assertAllStatus(t *testing.T, codes chan int, n, want int) {
	t.Helper()
	for range n {
		if code := <-codes; code != want {
			t.Errorf("one check returned %d, want %d", code, want)
		}
	}
}

func waitForInflight(t *testing.T, inflight *atomic.Int32, target int32) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		if inflight.Load() == target {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("only %d/%d analyses became inflight", inflight.Load(), target)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func waitForChan(t *testing.T, ch <-chan struct{}, timeout time.Duration, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for: %s", label)
	}
}

func checkBodyForFile(repo, file string) string {
	return `{"repo":` + jsonString(repo) + `,"file":` + jsonString(file) + `,"language":"go","proposed_content":"package p\n"}`
}

func fileN(n int) string {
	return "f" + string(rune('a'+n)) + ".go"
}

func writeRepoConfigWithErrorMode(t *testing.T, store *ConfigStore, repo string, mode EnforcementMode, errMode ErrorEnforcementMode, fitness map[string]bool) {
	t.Helper()
	cfg := Config{EnforcementMode: mode, EnforcementOnError: errMode, FitnessFunctions: fitness}
	content, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	writeRepoConfigContent(t, store, repo, string(content))
}

// testFixedWindowRateLimiter is a rate limiter with injectable time for tests.
type testFixedWindowRateLimiter struct {
	*fixedWindowRateLimiter
}

func newTestFixedWindowRateLimiter(limit int, windowSize time.Duration, now time.Time) *testFixedWindowRateLimiter {
	fl := &fixedWindowRateLimiter{
		limit:      limit,
		windowSize: windowSize,
		windows:    make(map[string]*callerWindow),
	}
	fl.nowFunc = func() time.Time { return now }
	return &testFixedWindowRateLimiter{fl}
}

func (l *testFixedWindowRateLimiter) setNow(t time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nowFunc = func() time.Time { return t }
}
