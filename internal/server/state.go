package server

import (
	"context"
	"sort"
	"sync"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
)

// State stores outstanding violations by repository and file.
type State struct {
	mu             sync.RWMutex
	violations     map[string]map[string][]fitness.Violation
	repoLocks      map[string]*repoLock
	warmups        map[string]warmupStatus
	warmupFailures map[string]string
}

type repoLock struct {
	permits chan struct{}
	refs    int
}

type warmupStatus int

const (
	warmupCold warmupStatus = iota
	warmupRunning
	warmupComplete
)

// NewState creates an empty outstanding violation store.
func NewState() *State {
	return &State{
		violations:     map[string]map[string][]fitness.Violation{},
		repoLocks:      map[string]*repoLock{},
		warmups:        map[string]warmupStatus{},
		warmupFailures: map[string]string{},
	}
}

// LockRepo serializes a check lifecycle for one repository (single-permit semaphore).
func (s *State) LockRepo(ctx context.Context, repo string) (func(), error) {
	return s.lockRepoN(ctx, repo, 1)
}

// LockRepoN acquires one permit from an N-wide semaphore for one repository.
// Returns a release function and nil on success, or nil and an error if ctx is
// canceled before a permit becomes available.
func (s *State) LockRepoN(ctx context.Context, repo string, n int) (func(), error) {
	return s.lockRepoN(ctx, repo, n)
}

// TryLockRepoN attempts to acquire one permit from an N-wide semaphore without
// blocking. Returns (release, true) if a permit was available, (nil, false) if
// all permits are already held.
func (s *State) TryLockRepoN(repo string, n int) (func(), bool) {
	if s == nil {
		return func() {}, true
	}
	lock := s.ensureRepoLock(repo, normalizePermits(n))
	select {
	case <-lock.permits:
		released := false
		return func() {
			if released {
				return
			}
			released = true
			lock.permits <- struct{}{}
			s.releaseRepoLock(repo, lock)
		}, true
	default:
		s.releaseRepoLock(repo, lock)
		return nil, false
	}
}

func (s *State) lockRepoN(ctx context.Context, repo string, n int) (func(), error) {
	if s == nil {
		return func() {}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	lock := s.ensureRepoLock(repo, normalizePermits(n))
	return s.acquireRepoPermit(ctx, repo, lock)
}

func normalizePermits(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

func (s *State) ensureRepoLock(repo string, n int) *repoLock {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.repoLocks == nil {
		s.repoLocks = map[string]*repoLock{}
	}
	lock := s.repoLocks[repo]
	if lock == nil || cap(lock.permits) != n {
		lock = newRepoLock(n)
		s.repoLocks[repo] = lock
	}
	lock.refs++
	return lock
}

func newRepoLock(n int) *repoLock {
	lock := &repoLock{permits: make(chan struct{}, n)}
	for range n {
		lock.permits <- struct{}{}
	}
	return lock
}

func (s *State) acquireRepoPermit(ctx context.Context, repo string, lock *repoLock) (func(), error) {
	select {
	case <-ctx.Done():
		s.releaseRepoLock(repo, lock)
		return nil, ctx.Err()
	case <-lock.permits:
		released := false
		return func() {
			if released {
				return
			}
			released = true
			lock.permits <- struct{}{}
			s.releaseRepoLock(repo, lock)
		}, nil
	}
}

func (s *State) releaseRepoLock(repo string, lock *repoLock) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lock.refs > 0 {
		lock.refs--
	}
	if lock.refs == 0 && s.repoLocks[repo] == lock {
		delete(s.repoLocks, repo)
	}
}

// BeginWarmup claims the one background warm-up slot for a language.
func (s *State) BeginWarmup(language string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.warmups == nil {
		s.warmups = map[string]warmupStatus{}
	}
	if s.warmups[language] != warmupCold {
		return false
	}
	s.warmups[language] = warmupRunning
	return true
}

// CompleteWarmup marks a language as ready for synchronous checks.
func (s *State) CompleteWarmup(language string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.warmups == nil {
		s.warmups = map[string]warmupStatus{}
	}
	s.warmups[language] = warmupComplete
	if s.warmupFailures != nil {
		delete(s.warmupFailures, language)
	}
}

// FailWarmup records a background warm-up failure for the next check to surface.
func (s *State) FailWarmup(language, message string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.warmups == nil {
		s.warmups = map[string]warmupStatus{}
	}
	if s.warmupFailures == nil {
		s.warmupFailures = map[string]string{}
	}
	s.warmups[language] = warmupCold
	s.warmupFailures[language] = message
}

// RecordWarmupFailure records a background warm-up failure for a language.
func (s *State) RecordWarmupFailure(language string, err error) {
	if s == nil || err == nil {
		return
	}
	s.FailWarmup(language, err.Error())
}

// TakeWarmupFailure returns and clears a background warm-up failure.
func (s *State) TakeWarmupFailure(language string) (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	message, ok := s.warmupFailures[language]
	if !ok {
		return "", false
	}
	delete(s.warmupFailures, language)
	return message, true
}

// IsWarm reports whether a language should use the synchronous check path.
func (s *State) IsWarm(language string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.warmups[language] == warmupComplete
}

// ReplaceFile replaces the outstanding violations for one repository file.
func (s *State) ReplaceFile(repo, file string, violations []fitness.Violation) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(violations) == 0 {
		if files, ok := s.violations[repo]; ok {
			delete(files, file)
			if len(files) == 0 {
				delete(s.violations, repo)
			}
		}
		return
	}
	if s.violations == nil {
		s.violations = map[string]map[string][]fitness.Violation{}
	}
	if _, ok := s.violations[repo]; !ok {
		s.violations[repo] = map[string][]fitness.Violation{}
	}
	s.violations[repo][file] = append([]fitness.Violation(nil), violations...)
}

// ClearRepo removes all outstanding violations for one repository.
func (s *State) ClearRepo(repo string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.violations, repo)
}

// HasFile reports whether a repository file has outstanding violations.
func (s *State) HasFile(repo, file string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	files, ok := s.violations[repo]
	if !ok {
		return false
	}
	return len(files[file]) > 0
}

// Violations returns all outstanding violations for one repository.
func (s *State) Violations(repo string) []fitness.Violation {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	files := s.violations[repo]
	violations := make([]fitness.Violation, 0)
	fileNames := make([]string, 0, len(files))
	for file := range files {
		fileNames = append(fileNames, file)
	}
	sort.Strings(fileNames)
	for _, file := range fileNames {
		fileViolations := files[file]
		violations = append(violations, fileViolations...)
	}
	sortViolations(violations)
	return violations
}

// Snapshot returns outstanding violations for all repositories.
func (s *State) Snapshot() map[string][]fitness.Violation {
	if s == nil {
		return map[string][]fitness.Violation{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := make(map[string][]fitness.Violation, len(s.violations))
	for repo, files := range s.violations {
		fileNames := make([]string, 0, len(files))
		for file := range files {
			fileNames = append(fileNames, file)
		}
		sort.Strings(fileNames)
		for _, file := range fileNames {
			fileViolations := files[file]
			snapshot[repo] = append(snapshot[repo], fileViolations...)
		}
		sortViolations(snapshot[repo])
	}
	return snapshot
}

func sortViolations(violations []fitness.Violation) {
	sort.SliceStable(violations, func(i, j int) bool {
		if violations[i].File != violations[j].File {
			return violations[i].File < violations[j].File
		}
		if violations[i].Function != violations[j].Function {
			return violations[i].Function < violations[j].Function
		}
		return violations[i].FitnessFunction < violations[j].FitnessFunction
	})
}

// violationLedger is the outstanding-violation store as the scoring path sees
// it. A real check writes through to State; a dry run — an agent's speculative
// proposal, a doctor probe — computes the same verdict and writes nothing,
// because content that may never land on disk must never poison the ledger the
// next commit is judged against.
//
// Scope decision (calm-poc-cpvk, recorded on the issue): only speculative
// proposals dry-run. The commit-path hooks stay wet, and the repository-wide
// aggregation they feed is deliberate — the ledger is the blackboard on which
// one file's violation keeps the whole repository blocked until it is fixed.
// Making every check non-persistent would delete that governance model, not
// improve it; the fix was to stop counting proposals that never landed.
type violationLedger struct {
	state  *State
	dryRun bool
}

func newViolationLedger(state *State, dryRun bool) violationLedger {
	return violationLedger{state: state, dryRun: dryRun}
}

// ReplaceFile records one file's outstanding violations, unless this check is a
// dry run.
func (l violationLedger) ReplaceFile(repo, file string, violations []fitness.Violation) {
	if l.dryRun {
		return
	}
	l.state.ReplaceFile(repo, file, violations)
}

// ClearRepo drops a repository's outstanding violations, unless this check is a
// dry run: a dry run is a pure read, so it neither adds entries nor clears the
// ones a real check recorded.
func (l violationLedger) ClearRepo(repo string) {
	if l.dryRun {
		return
	}
	l.state.ClearRepo(repo)
}

// OutstandingAfter is the set the caller is told about once file has been
// scored with proposed. For a real check the write has already landed, so the
// answer is simply the ledger's contents and the two arguments only describe
// how it got there; for a dry run they are the withheld write itself, applied
// to a copy — proposed standing in for whatever the ledger holds for file.
func (l violationLedger) OutstandingAfter(repo, file string, proposed []fitness.Violation) []fitness.Violation {
	if !l.dryRun {
		return l.state.Violations(repo)
	}
	outstanding := l.state.Violations(repo)
	simulated := make([]fitness.Violation, 0, len(outstanding)+len(proposed))
	for _, violation := range outstanding {
		if violation.File != file {
			simulated = append(simulated, violation)
		}
	}
	simulated = append(simulated, proposed...)
	sortViolations(simulated)
	return simulated
}
