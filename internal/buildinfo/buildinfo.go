// Package buildinfo exposes the binary's VCS identity (revision and dirty
// flag) so the client and server can compare builds without re-reading
// runtime/debug in multiple places.
package buildinfo

import "runtime/debug"

// FromBuildInfo extracts the VCS revision (truncated to 12 characters) and the
// dirty-worktree flag from a build-info record.
func FromBuildInfo(info *debug.BuildInfo) (revision string, modified bool) {
	// Stub pending S2 implementation (calm-poc-fuz1).
	return "", false
}

// Current reports the running binary's own VCS identity, or empty values when
// the binary was built without VCS stamping.
func Current() (revision string, modified bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	return FromBuildInfo(info)
}
