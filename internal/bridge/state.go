package bridge

import "sync"

// State stores outstanding violations by repository and file.
type State struct {
	mu         sync.RWMutex
	violations map[string]map[string][]Violation
}

// NewState creates an empty outstanding violation store.
func NewState() *State {
	return &State{violations: map[string]map[string][]Violation{}}
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
	for _, fileViolations := range files {
		violations = append(violations, fileViolations...)
	}
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
		for _, fileViolations := range files {
			snapshot[repo] = append(snapshot[repo], fileViolations...)
		}
	}
	return snapshot
}
