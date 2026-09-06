package buildinfo

import (
	"runtime/debug"
	"testing"
)

// S2 red-test contract (calm-poc-fuz1): FromBuildInfo extracts the vcs.revision
// setting truncated to 12 characters and the vcs.modified dirty flag.

func TestFromBuildInfoExtractsRevisionAndModified(t *testing.T) {
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "e758e98da399f00dcafe1234567890abcdef1234"},
		{Key: "vcs.modified", Value: "true"},
	}}
	revision, modified := FromBuildInfo(info)
	if revision != "e758e98da399" {
		t.Errorf("revision = %q, want the 12-character truncation %q", revision, "e758e98da399")
	}
	if !modified {
		t.Error("modified = false, want true when vcs.modified is true")
	}
}

func TestFromBuildInfoShortRevisionAndCleanTree(t *testing.T) {
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "abc123"},
		{Key: "vcs.modified", Value: "false"},
	}}
	revision, modified := FromBuildInfo(info)
	if revision != "abc123" {
		t.Errorf("revision = %q, want short revisions passed through unchanged", revision)
	}
	if modified {
		t.Error("modified = true, want false when vcs.modified is false")
	}
}

func TestFromBuildInfoWithoutVCSSettings(t *testing.T) {
	revision, modified := FromBuildInfo(&debug.BuildInfo{})
	if revision != "" || modified {
		t.Errorf("got (%q, %v), want empty identity when the build carries no VCS settings", revision, modified)
	}
}
