# Fix Bridge CALM Violations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the four pre-existing CALM fitness function violations in the `internal/bridge` package so that edits to the package pass the pre-tool-use hook, then add `fmt.Println("ready")` to the warmup completion path.

**Architecture:** Two violations are bugs in the analyzer/aggregator (test files included in `interface_width` count; `analyzeGoWithModuleContext` lacks a `_test.go` exclusion filter). Four violations are real complexity issues in `checker.go` — `Check`, `checkSynchronousLocked`, `analyzeGoWithModuleContext`, and `validSourceExtension` all exceed cyclomatic complexity 9.

**Tech Stack:** Go 1.21+, `go/ast`, `gocyclo`, `testing`, project conventions (no external test helpers, table-driven tests, `t.TempDir()`).

---

## Violations to fix

| Fitness function | Location | Current | Limit | Root cause |
|---|---|---|---|---|
| `interface_width` | `internal/bridge/checker.go` | 87 | 20 | `analyzeGoWithModuleContext` includes `_test.go` files when scanning the module directory — test functions are exported (`Test*`), inflating the count |
| `cyclomatic_complexity` | `Check` | 16 | 9 | Six `if` branches + error checks inline |
| `cyclomatic_complexity` | `checkSynchronousLocked` | 24 | 9 | Deep nesting; many inline error/switch branches |
| `cyclomatic_complexity` | `analyzeGoWithModuleContext` | 12 | 9 | Nested `for` + multiple `if`/`continue` branches |
| `cyclomatic_complexity` | `validSourceExtension` | 13 | 9 | Inline character-range checks |

---

## File Structure

| File | Change |
|---|---|
| `internal/analyzer/golang.go` | Fix: exclude `_test.go` files in `analyzeGoWithModuleContext` (already excludes `_generated.go` — same pattern) |
| `internal/bridge/checker.go` | Refactor: extract warm-csharp guard (`checkWithCSharpWarmGuard`), extract analysis step (`analyzeSource`), extract validation pipeline (`runValidationAndScore`), extract char validation (`isValidExtensionChar`), extract peer-file scan (`collectPeerGoFiles`); add `fmt.Println("ready")` in `startDeferredCheck` |
| `internal/bridge/checker_test.go` | Tests: add/adjust tests that cover the extracted functions and verify the `ready` stdout output |
| `internal/analyzer/golang_test.go` | Tests: add test asserting `_test.go` files are excluded from module metric aggregation |

---

## Task 1: Fix `interface_width` — exclude `_test.go` from module context scan

**Files:**
- Modify: `internal/analyzer/golang.go` (`analyzeGoWithModuleContext` loop, lines 551–566)
- Test: `internal/analyzer/golang_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/analyzer/golang_test.go`:

```go
func TestAnalyzeGoWithModuleContextExcludesTestFiles(t *testing.T) {
    dir := t.TempDir()
    prodSrc := "package bridge\n\nfunc PublicOne() {}\nfunc PublicTwo() {}\n"
    var testFns strings.Builder
    testFns.WriteString("package bridge\n\nimport \"testing\"\n\n")
    for i := range 50 {
        fmt.Fprintf(&testFns, "func TestFoo%d(t *testing.T) { _ = t }\n", i)
    }
    if err := os.WriteFile(filepath.Join(dir, "bridge.go"), []byte(prodSrc), 0600); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(dir, "bridge_test.go"), []byte(testFns.String()), 0600); err != nil {
        t.Fatal(err)
    }
    result, err := AnalyzeGoFile(filepath.Join(dir, "bridge.go"))
    if err != nil {
        t.Fatalf("AnalyzeGoFile: %v", err)
    }
    // Simulate module-level aggregation as analyzeGoWithModuleContext does
    request := AnalysisRequest{
        Repo:     dir,
        File:     "bridge.go",
        Language: "go",
        TempPath: filepath.Join(dir, "bridge.go"),
    }
    aggregated, err := analyzeGoWithModuleContext(context.Background(), request)
    if err != nil {
        t.Fatalf("analyzeGoWithModuleContext: %v", err)
    }
    if aggregated.ModuleMetric.PublicMethods != result.ModuleMetric.PublicMethods {
        t.Errorf("PublicMethods = %d, want %d (test files must not inflate count)",
            aggregated.ModuleMetric.PublicMethods, result.ModuleMetric.PublicMethods)
    }
}
```

Note: `analyzeGoWithModuleContext` is package-private — this test lives in `package analyzer` (same package) so it can call it directly.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/poconnor/peter_code/calm-poc
go test ./internal/analyzer/... -run TestAnalyzeGoWithModuleContextExcludesTestFiles -v
```

Expected: `FAIL — PublicMethods = 52, want 2`

- [ ] **Step 3: Fix `analyzeGoWithModuleContext` in `internal/analyzer/golang.go`**

Change the directory scan loop (lines 551–566) to skip `_test.go` files:

```go
for _, entry := range dirEntries {
    if entry.IsDir() ||
        !strings.HasSuffix(entry.Name(), ".go") ||
        strings.HasSuffix(entry.Name(), "_generated.go") ||
        strings.HasSuffix(entry.Name(), "_test.go") {
        continue
    }
    path := filepath.Join(filepath.Dir(logicalPath), entry.Name())
    if filepath.Clean(path) == filepath.Clean(logicalPath) {
        continue
    }
    existing, err := analyzer.AnalyzeGoFile(path)
    if err != nil {
        return analyzer.AnalysisResult{}, err
    }
    if existing.StackNode == proposed.StackNode {
        results = append(results, existing)
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/analyzer/... -run TestAnalyzeGoWithModuleContextExcludesTestFiles -v
```

Expected: `PASS`

- [ ] **Step 5: Run the full test suite to confirm no regressions**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 6: Verify `interface_width` is cleared against the real file**

```bash
cp internal/bridge/checker.go /tmp/checker_proposed.go
/tmp/calm-bridge check \
  --file internal/bridge/checker.go \
  --repo /Users/poconnor/peter_code/calm-poc \
  --content-file /tmp/checker_proposed.go \
  --language go 2>&1
```

Expected: `interface_width` violation is gone from the output. Complexity violations will still be present.

- [ ] **Step 7: Commit**

```bash
git add internal/analyzer/golang.go internal/analyzer/golang_test.go
git commit -m "fix(analyzer): exclude _test.go files from module-level public method count

Test functions (Test*) are exported by convention but are not part of the
module's public interface. Including them caused interface_width violations
to fire against production code that was within limits.

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-sonnet-4-6"
```

---

## Task 2: Fix `cyclomatic_complexity` in `validSourceExtension`

`validSourceExtension` has complexity 13 from inline character-range `if` conditions. Extract a helper.

**Files:**
- Modify: `internal/bridge/checker.go:601-612`
- Test: `internal/bridge/checker_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/bridge/checker_test.go`:

```go
func TestIsValidExtensionChar(t *testing.T) {
    tests := []struct {
        char rune
        want bool
    }{
        {'.', true},
        {'_', true},
        {'-', true},
        {'0', true},
        {'9', true},
        {'a', true},
        {'z', true},
        {'A', true},
        {'Z', true},
        {'/', false},
        {' ', false},
        {0, false},
        {'!', false},
    }
    for _, tc := range tests {
        if got := isValidExtensionChar(tc.char); got != tc.want {
            t.Errorf("isValidExtensionChar(%q) = %v, want %v", tc.char, got, tc.want)
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/bridge/... -run TestIsValidExtensionChar -v
```

Expected: `FAIL — isValidExtensionChar undefined`

- [ ] **Step 3: Extract `isValidExtensionChar` and simplify `validSourceExtension`**

Replace the existing `validSourceExtension` function in `internal/bridge/checker.go` with:

```go
func validSourceExtension(extension string) bool {
    if len(extension) > 16 || strings.ContainsRune(extension, 0) {
        return false
    }
    for _, char := range extension {
        if !isValidExtensionChar(char) {
            return false
        }
    }
    return true
}

func isValidExtensionChar(char rune) bool {
    return char == '.' || char == '_' || char == '-' ||
        char >= '0' && char <= '9' ||
        char >= 'A' && char <= 'Z' ||
        char >= 'a' && char <= 'z'
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/bridge/... -run TestIsValidExtensionChar -v
```

Expected: `PASS`

- [ ] **Step 5: Run the full suite**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/bridge/checker.go internal/bridge/checker_test.go
git commit -m "refactor(bridge): extract isValidExtensionChar to reduce cyclomatic complexity

validSourceExtension had complexity 13 from inline char-range conditions.
Extracted isValidExtensionChar helper brings both functions within the limit of 9.

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-sonnet-4-6"
```

---

## Task 3: Fix `cyclomatic_complexity` in `analyzeGoWithModuleContext`

`analyzeGoWithModuleContext` has complexity 12. The nested `for` loop with multiple `continue` branches drives this. Extract peer-file collection into a helper.

**Files:**
- Modify: `internal/bridge/checker.go:534-568`
- Test: `internal/bridge/checker_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/bridge/checker_test.go`:

```go
func TestCollectPeerGoFilesExcludesGeneratedAndTestFiles(t *testing.T) {
    dir := t.TempDir()
    files := map[string]string{
        "main.go":             "package main\n",
        "other.go":            "package main\n",
        "gen_generated.go":    "package main\n",
        "main_test.go":        "package main\n",
    }
    for name, content := range files {
        if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
            t.Fatal(err)
        }
    }
    got, err := collectPeerGoFiles(dir, filepath.Join(dir, "main.go"))
    if err != nil {
        t.Fatalf("collectPeerGoFiles: %v", err)
    }
    for _, f := range got {
        base := filepath.Base(f)
        if strings.HasSuffix(base, "_generated.go") || strings.HasSuffix(base, "_test.go") {
            t.Errorf("collectPeerGoFiles returned excluded file: %s", f)
        }
        if base == "main.go" {
            t.Errorf("collectPeerGoFiles returned the proposed file itself")
        }
    }
    if len(got) != 1 {
        t.Errorf("len(got) = %d, want 1 (only other.go); got %v", len(got), got)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/bridge/... -run TestCollectPeerGoFilesExcludesGeneratedAndTestFiles -v
```

Expected: `FAIL — collectPeerGoFiles undefined`

- [ ] **Step 3: Extract `collectPeerGoFiles` and simplify `analyzeGoWithModuleContext`**

Add the following function to `internal/bridge/checker.go` (after `analyzeGoWithModuleContext`):

```go
// collectPeerGoFiles returns peer .go files in dir, excluding the proposed file,
// generated files, and test files.
func collectPeerGoFiles(dir, logicalPath string) ([]string, error) {
    entries, err := os.ReadDir(dir)
    if err != nil {
        return nil, err
    }
    var peers []string
    for _, entry := range entries {
        name := entry.Name()
        if entry.IsDir() ||
            !strings.HasSuffix(name, ".go") ||
            strings.HasSuffix(name, "_generated.go") ||
            strings.HasSuffix(name, "_test.go") {
            continue
        }
        path := filepath.Join(dir, name)
        if filepath.Clean(path) == filepath.Clean(logicalPath) {
            continue
        }
        peers = append(peers, path)
    }
    return peers, nil
}
```

Replace `analyzeGoWithModuleContext` with:

```go
func analyzeGoWithModuleContext(ctx context.Context, request AnalysisRequest) (analyzer.AnalysisResult, error) {
    if err := ctx.Err(); err != nil {
        return analyzer.AnalysisResult{}, err
    }
    proposed, err := analyzer.AnalyzeGoFile(request.TempPath)
    if err != nil {
        return analyzer.AnalysisResult{}, err
    }
    if err := ctx.Err(); err != nil {
        return analyzer.AnalysisResult{}, err
    }
    logicalPath := filepath.Join(request.Repo, request.File)
    peers, err := collectPeerGoFiles(filepath.Dir(logicalPath), logicalPath)
    if err != nil {
        return proposed, nil
    }
    results := []analyzer.AnalysisResult{proposed}
    for _, path := range peers {
        existing, err := analyzer.AnalyzeGoFile(path)
        if err != nil {
            return analyzer.AnalysisResult{}, err
        }
        if existing.StackNode == proposed.StackNode {
            results = append(results, existing)
        }
    }
    return analyzer.AggregateModuleMetrics(results)[0], nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/bridge/... -run TestCollectPeerGoFilesExcludesGeneratedAndTestFiles -v
```

Expected: `PASS`

- [ ] **Step 5: Run the full suite**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/bridge/checker.go internal/bridge/checker_test.go
git commit -m "refactor(bridge): extract collectPeerGoFiles to reduce analyzeGoWithModuleContext complexity

analyzeGoWithModuleContext had cyclomatic complexity 12. Extracting peer-file
collection into collectPeerGoFiles brings both functions within the limit of 9.

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-sonnet-4-6"
```

---

## Task 4: Fix `cyclomatic_complexity` in `checkSynchronousLocked`

`checkSynchronousLocked` has complexity 24. Extract source analysis and validation+scoring into helpers.

**Files:**
- Modify: `internal/bridge/checker.go:147-246`
- Test: `internal/bridge/checker_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/bridge/checker_test.go`:

```go
func TestAnalyzeSourceReturnsInputErrorForUnsupportedLanguage(t *testing.T) {
    repo := t.TempDir()
    checker := Checker{State: NewState()}
    _, err := checker.analyzeSource(context.Background(), CheckRequest{
        Repo:     repo,
        File:     "main.rb",
        Language: "ruby",
    }, repo, filepath.Join(repo, "main.rb"))
    if err == nil {
        t.Fatal("expected error for unsupported language")
    }
    var checkErr *CheckError
    if !errors.As(err, &checkErr) || checkErr.Kind != ErrorKindInput {
        t.Errorf("err = %v, want CheckError with Kind=input", err)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/bridge/... -run TestAnalyzeSourceReturnsInputErrorForUnsupportedLanguage -v
```

Expected: `FAIL — analyzeSource undefined`

- [ ] **Step 3: Extract `analyzeSource`, `classifyAnalysisError`, and `runValidationAndScore`**

Add these functions to `internal/bridge/checker.go` (after `checkSynchronousLocked`):

```go
// analyzeSource runs the language-specific analyzer for the proposed file content.
func (c *Checker) analyzeSource(ctx context.Context, request CheckRequest, repo, sourcePath string) (analyzer.AnalysisResult, error) {
    sourceAnalyzer, ok := c.sourceAnalyzer(request.Language)
    if !ok {
        return analyzer.AnalysisResult{}, inputError(fmt.Sprintf("unsupported language %q", request.Language), nil)
    }
    if err := ctx.Err(); err != nil {
        return analyzer.AnalysisResult{}, infrastructureError("check canceled before analysis", err)
    }
    result, err := sourceAnalyzer.Analyze(ctx, AnalysisRequest{
        Repo:     repo,
        File:     request.File,
        Language: request.Language,
        TempPath: sourcePath,
    })
    if err != nil {
        return analyzer.AnalysisResult{}, classifyAnalysisError(err, request.Language)
    }
    if err := ctx.Err(); err != nil {
        return analyzer.AnalysisResult{}, infrastructureError("check canceled after analysis", err)
    }
    return result, nil
}

func classifyAnalysisError(err error, language string) error {
    var checkErr *CheckError
    if errors.As(err, &checkErr) {
        return checkErr
    }
    if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
        return infrastructureError("check canceled during analysis", err)
    }
    if isAnalyzerInfrastructureError(err) {
        return infrastructureError(fmt.Sprintf("running %s analyzer", language), err)
    }
    return inputError(fmt.Sprintf("analyzing %s file", language), err)
}

// runValidationAndScore runs CALM validation and fitness scoring on an analyzed result.
func (c *Checker) runValidationAndScore(ctx context.Context, result analyzer.AnalysisResult, repo, file, patternPath string, config Config, state *State) (CheckResponse, error) {
    pattern, err := calm.LoadPattern(patternPath)
    if err != nil {
        return CheckResponse{}, infrastructureError("loading governance pattern", err)
    }
    architecturePath, cleanupArchitecture, err := c.writeArchitecture(report.BuildArchitecture(result))
    if err != nil {
        return CheckResponse{}, err
    }
    defer func() { _ = cleanupArchitecture() }()
    validator := c.Validator
    switch {
    case validator == nil:
        validator = calm.Validator{}
    case isNilInterface(validator):
        return CheckResponse{}, infrastructureError("CALM validator is not configured", nil)
    }
    validation, err := validator.Validate(ctx, architecturePath, patternPath)
    if err != nil && !isValidationFailure(validation) {
        return CheckResponse{}, infrastructureError("running CALM validation", err)
    }
    violations := filterViolations(fitnessViolations(result, pattern), config)
    if len(violations) == 0 {
        return c.scoreClean(repo, file, config, state)
    }
    return c.scoreDirty(repo, file, violations, config, state)
}

func (c *Checker) scoreClean(repo, file string, config Config, state *State) (CheckResponse, error) {
    if config.EnforcementMode == EnforcementBlock {
        state.ReplaceFile(repo, file, nil)
        if outstanding := state.Violations(repo); len(outstanding) > 0 {
            return CheckResponse{Status: StatusBlock, Violations: outstanding}, nil
        }
    } else {
        state.ClearRepo(repo)
    }
    return CheckResponse{Status: StatusPass}, nil
}

func (c *Checker) scoreDirty(repo, file string, violations []Violation, config Config, state *State) (CheckResponse, error) {
    switch config.EnforcementMode {
    case EnforcementAdvisory:
        state.ClearRepo(repo)
        return CheckResponse{Status: StatusAdvisory, Violations: violations}, nil
    default:
        state.ReplaceFile(repo, file, violations)
        return CheckResponse{Status: StatusBlock, Violations: state.Violations(repo)}, nil
    }
}
```

Replace `checkSynchronousLocked` with:

```go
func (c *Checker) checkSynchronousLocked(ctx context.Context, request CheckRequest, repo string, config Config, state *State) (response CheckResponse, err error) {
    patternPath, cleanupPattern, err := c.resolvePatternPath()
    if err != nil {
        return CheckResponse{}, err
    }
    defer func() {
        if cleanupErr := cleanupPattern(); cleanupErr != nil {
            err = errors.Join(err, infrastructureError("cleaning temp pattern file", cleanupErr))
        }
    }()
    sourcePath, cleanup, err := c.writeProposedContent(request)
    if err != nil {
        return CheckResponse{}, err
    }
    defer func() {
        if cleanupErr := cleanup(); cleanupErr != nil {
            err = errors.Join(err, infrastructureError("cleaning temporary source file", cleanupErr))
        }
    }()
    result, err := c.analyzeSource(ctx, request, repo, sourcePath)
    if err != nil {
        return CheckResponse{}, err
    }
    result = analyzer.EnsureModuleMetric(result)
    result.File = request.File
    result.StackNode = calmNodeForRequest(request, result.StackNode)
    return c.runValidationAndScore(ctx, result, repo, request.File, patternPath, config, state)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/bridge/... -run TestAnalyzeSourceReturnsInputErrorForUnsupportedLanguage -v
```

Expected: `PASS`

- [ ] **Step 5: Run the full suite**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 6: Check complexity of `checkSynchronousLocked` has dropped**

```bash
cp internal/bridge/checker.go /tmp/checker_proposed.go
/tmp/calm-bridge check \
  --file internal/bridge/checker.go \
  --repo /Users/poconnor/peter_code/calm-poc \
  --content-file /tmp/checker_proposed.go \
  --language go 2>&1
```

Expected: `checkSynchronousLocked` complexity violation is gone. `Check` violation may remain — Task 5 handles that.

- [ ] **Step 7: Commit**

```bash
git add internal/bridge/checker.go internal/bridge/checker_test.go
git commit -m "refactor(bridge): extract analyzeSource and runValidationAndScore from checkSynchronousLocked

checkSynchronousLocked had cyclomatic complexity 24. Extracting source analysis
and validation+scoring pipelines brings all functions within the limit of 9.

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-sonnet-4-6"
```

---

## Task 5: Fix `cyclomatic_complexity` in `Check`

`Check` has complexity 16 from the inline csharp warmup sequencing. Extract it.

**Files:**
- Modify: `internal/bridge/checker.go:90-136`
- Test: `internal/bridge/checker_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/bridge/checker_test.go`:

```go
func TestCheckWithCSharpWarmGuardPassesWhileWarming(t *testing.T) {
    repo := t.TempDir()
    writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
    patternPath := writeTestPattern(t)
    deferredCtx, cancel := context.WithCancel(context.Background())
    defer cancel()
    checker := Checker{
        PatternPath:     patternPath,
        Validator: validatorFunc(func(_ context.Context, _, _ string) (calm.ValidationResult, error) {
            return calm.ValidationResult{Valid: true}, nil
        }),
        State:           NewState(),
        DeferredContext: deferredCtx,
        Analyzers: map[string]SourceAnalyzer{
            "csharp": AnalyzerFunc(func(_ context.Context, _ AnalysisRequest) (analyzer.AnalysisResult, error) {
                return analyzer.AnalysisResult{
                    StackNode:  "warmup",
                    Language:  "csharp",
                    Functions: []analyzer.FunctionMetric{{Name: "Run", CyclomaticComplexity: 1, IsPublic: true, LOC: 5}},
                }, nil
            }),
        },
    }
    response, err := checker.Check(context.Background(), CheckRequest{
        Repo:            repo,
        File:            "src/Warmup.cs",
        Language:        "csharp",
        ProposedContent: "// warmup",
    })
    if err != nil {
        t.Fatalf("Check: %v", err)
    }
    if response.Status != StatusPass {
        t.Errorf("status = %q, want %q", response.Status, StatusPass)
    }
    if !response.Warming {
        t.Error("Warming = false, want true on first csharp check")
    }
}
```

- [ ] **Step 2: Run test to verify it passes already (or fails with compilation error)**

```bash
go test ./internal/bridge/... -run TestCheckWithCSharpWarmGuardPassesWhileWarming -v
```

This test may pass already since `Check` already has this path — if so that confirms `Check` covers it and we proceed to the refactor.

- [ ] **Step 3: Extract `checkWithCSharpWarmGuard` from `Check`**

Replace `Check` with:

```go
func (c *Checker) Check(ctx context.Context, request CheckRequest) (response CheckResponse, err error) {
    if c.State == nil {
        return CheckResponse{}, infrastructureError("checker state is not configured", nil)
    }
    config, repo, err := loadConfig(request.Repo)
    if err != nil {
        return CheckResponse{}, err
    }
    state := c.state()
    if config.EnforcementMode == EnforcementOff {
        state.ClearRepo(repo)
        return CheckResponse{Status: StatusPass}, nil
    }
    if request.Language == "csharp" && config.EnforcementMode == EnforcementBlock {
        return c.checkWithCSharpWarmGuard(ctx, request, repo, config, state)
    }
    return c.checkSynchronous(ctx, request, repo, config, state)
}

// checkWithCSharpWarmGuard handles csharp warmup sequencing before delegating
// to the synchronous check path.
func (c *Checker) checkWithCSharpWarmGuard(ctx context.Context, request CheckRequest, repo string, config Config, state *State) (CheckResponse, error) {
    if message, ok := state.TakeWarmupFailure("csharp"); ok {
        return CheckResponse{}, infrastructureError("csharp warm-up failed", errors.New(message))
    }
    if outstanding := state.Violations(repo); hasOtherFileViolation(outstanding, request.File) {
        return CheckResponse{Status: StatusBlock, Violations: outstanding}, nil
    }
    if state.IsWarm("csharp") {
        return c.checkSynchronous(ctx, request, repo, config, state)
    }
    unlockRepo, err := state.LockRepo(ctx, repo)
    if err != nil {
        return CheckResponse{}, infrastructureError("check canceled while waiting for repository lock", err)
    }
    if state.IsWarm("csharp") {
        defer unlockRepo()
        return c.checkSynchronousLocked(ctx, request, repo, config, state)
    }
    if message, ok := state.TakeWarmupFailure("csharp"); ok {
        unlockRepo()
        return CheckResponse{}, infrastructureError("csharp warm-up failed", errors.New(message))
    }
    if outstanding := state.Violations(repo); hasOtherFileViolation(outstanding, request.File) {
        unlockRepo()
        return CheckResponse{Status: StatusBlock, Violations: outstanding}, nil
    }
    if state.BeginWarmup("csharp") {
        c.startDeferredCheck(request, repo, config, unlockRepo)
        return CheckResponse{Status: StatusPass, Warming: true}, nil
    }
    defer unlockRepo()
    return c.checkSynchronousLocked(ctx, request, repo, config, state)
}
```

- [ ] **Step 4: Run test to verify it still passes**

```bash
go test ./internal/bridge/... -run TestCheckWithCSharpWarmGuardPassesWhileWarming -v
```

Expected: `PASS`

- [ ] **Step 5: Run the full suite**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 6: Verify all CALM violations are cleared**

```bash
cp internal/bridge/checker.go /tmp/checker_proposed.go
/tmp/calm-bridge check \
  --file internal/bridge/checker.go \
  --repo /Users/poconnor/peter_code/calm-poc \
  --content-file /tmp/checker_proposed.go \
  --language go 2>&1
```

Expected: `{"status":"pass"}` — no violations.

- [ ] **Step 7: Commit**

```bash
git add internal/bridge/checker.go internal/bridge/checker_test.go
git commit -m "refactor(bridge): extract checkWithCSharpWarmGuard to reduce Check complexity

Check had cyclomatic complexity 16 from inline csharp warmup sequencing.
Extracting checkWithCSharpWarmGuard brings both functions within the limit of 9.

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-sonnet-4-6"
```

---

## Task 6: Add `fmt.Println("ready")` warmup signal

**Files:**
- Modify: `internal/bridge/checker.go` (`startDeferredCheck`)
- Test: `internal/bridge/checker_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/bridge/checker_test.go`:

```go
func TestStartDeferredCheckPrintsReadyOnSuccess(t *testing.T) {
    repo := t.TempDir()
    writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
    patternPath := writeTestPattern(t)
    deferredCtx, cancel := context.WithCancel(context.Background())
    defer cancel()

    r, w, err := os.Pipe()
    if err != nil {
        t.Fatal(err)
    }
    old := os.Stdout
    os.Stdout = w

    checker := Checker{
        PatternPath:     patternPath,
        Validator: validatorFunc(func(_ context.Context, _, _ string) (calm.ValidationResult, error) {
            return calm.ValidationResult{Valid: true}, nil
        }),
        State:           NewState(),
        DeferredContext: deferredCtx,
        Analyzers: map[string]SourceAnalyzer{
            "csharp": AnalyzerFunc(func(_ context.Context, _ AnalysisRequest) (analyzer.AnalysisResult, error) {
                return analyzer.AnalysisResult{
                    StackNode:  "warmup",
                    Language:  "csharp",
                    Functions: []analyzer.FunctionMetric{{Name: "Run", CyclomaticComplexity: 1, IsPublic: true, LOC: 5}},
                }, nil
            }),
        },
    }
    state := checker.State
    unlockRepo, lockErr := state.LockRepo(context.Background(), repo)
    if lockErr != nil {
        t.Fatal(lockErr)
    }
    done := make(chan struct{})
    config := defaultConfig()
    checker.startDeferredCheck(
        CheckRequest{Repo: repo, File: "src/Warmup.cs", Language: "csharp", ProposedContent: "// warmup"},
        repo, config, func() { unlockRepo(); close(done) },
    )
    <-done

    _ = w.Close()
    os.Stdout = old
    var buf bytes.Buffer
    _, _ = io.Copy(&buf, r)
    if !strings.Contains(buf.String(), "ready") {
        t.Errorf("stdout = %q, want to contain \"ready\"", buf.String())
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/bridge/... -run TestStartDeferredCheckPrintsReadyOnSuccess -v
```

Expected: `FAIL — stdout does not contain "ready"`

- [ ] **Step 3: Add `fmt.Println("ready")` in `startDeferredCheck`**

In `internal/bridge/checker.go`, after `checker.State.CompleteWarmup("csharp")`:

```go
checker.State.CompleteWarmup("csharp")
fmt.Println("ready")
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/bridge/... -run TestStartDeferredCheckPrintsReadyOnSuccess -v
```

Expected: `PASS`

- [ ] **Step 5: Run the full suite**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/bridge/checker.go internal/bridge/checker_test.go
git commit -m "feat(bridge): print 'ready' to stdout when csharp cache warms up

Allows callers (scripts, VS Code tasks) to know when calm-serve has completed
its background csharp warmup by watching stdout for the 'ready' line.

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude Code <noreply@anthropic.com> - claude-sonnet-4-6"
```

---

## Self-Review

**Spec coverage:**
- `interface_width: 87` → fixed by excluding `_test.go` in Task 1 ✓
- `cyclomatic_complexity: validSourceExtension` → fixed in Task 2 ✓
- `cyclomatic_complexity: analyzeGoWithModuleContext` → fixed in Task 3 ✓
- `cyclomatic_complexity: checkSynchronousLocked` → fixed in Task 4 ✓
- `cyclomatic_complexity: Check` → fixed in Task 5 ✓
- `fmt.Println("ready")` → added in Task 6 ✓

**Placeholder scan:** All tasks have complete code. No TBDs.

**Type consistency:**
- `runValidationAndScore` signature in Task 4 is consistent with Task 5's `checkSynchronousLocked` which calls it. ✓
- `checkWithCSharpWarmGuard` in Task 5 matches the call site in `Check`. ✓
- `collectPeerGoFiles(dir, logicalPath string)` in Task 3 matches its test. ✓
- `analyzeSource(ctx, request, repo, sourcePath)` in Task 4 matches its test. ✓
- `scoreClean` and `scoreDirty` are private helpers not referenced outside Task 4. ✓
