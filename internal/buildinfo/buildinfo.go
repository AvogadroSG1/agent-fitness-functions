// Package buildinfo exposes the binary's VCS identity (revision and dirty
// flag) so the client and server can compare builds without re-reading
// runtime/debug in multiple places.
package buildinfo

import "runtime/debug"

// revisionLength is how much of the VCS revision identifies a build in
// human-facing output (health bodies, doctor lines).
const revisionLength = 12

// FromBuildInfo extracts the VCS revision (truncated to 12 characters) and the
// dirty-worktree flag from a build-info record.
func FromBuildInfo(info *debug.BuildInfo) (revision string, modified bool) {
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = truncate(setting.Value, revisionLength)
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	return revision, modified
}

func truncate(value string, length int) string {
	if len(value) > length {
		return value[:length]
	}
	return value
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
