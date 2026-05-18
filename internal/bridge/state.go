package bridge

import (
	"sort"
	"sync"
)

// State stores outstanding violations by repository and file.
type State struct {
	mu         sync.RWMutex
	violations map[string]map[string][]Violation
	repoLocks  map[string]*sync.Mutex
}

// NewState creates an empty outstanding violation store.
func NewState() *State {
	return &State{
		violations: map[string]map[string][]Violation{},
		repoLocks:  map[string]*sync.Mutex{},
	}
}

// LockRepo serializes a check lifecycle for one repository.
func (s *State) LockRepo(repo string) func() {
	if s == nil {
		return func() {}
	}
	s.mu.Lock()
	if s.repoLocks == nil {
		s.repoLocks = map[string]*sync.Mutex{}
	}
	lock := s.repoLocks[repo]
	if lock == nil {
		lock = &sync.Mutex{}
		s.repoLocks[repo] = lock
	}
	s.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

// ReplaceFile replaces the outstanding violations for one repository file.
func (s *State) ReplaceFile(repo, file string, violations []Violation) {
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
		s.violations = map[string]map[string][]Violation{}
	}
	if _, ok := s.violations[repo]; !ok {
		s.violations[repo] = map[string][]Violation{}
	}
	s.violations[repo][file] = append([]Violation(nil), violations...)
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
func (s *State) Violations(repo string) []Violation {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	files := s.violations[repo]
	violations := make([]Violation, 0)
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
func (s *State) Snapshot() map[string][]Violation {
	if s == nil {
		return map[string][]Violation{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := make(map[string][]Violation, len(s.violations))
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

func sortViolations(violations []Violation) {
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
