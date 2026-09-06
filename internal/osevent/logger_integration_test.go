//go:build integration && (darwin || linux)

package osevent

import (
	"context"
	"os"
	"testing"
)

func TestNativeServiceLogger(t *testing.T) {
	marker := os.Getenv("AGENT_FITNESS_FUNCTIONS_NATIVE_LOG_MARKER")
	if marker == "" {
		t.Skip("AGENT_FITNESS_FUNCTIONS_NATIVE_LOG_MARKER is not set")
	}
	if err := LogService(context.Background(), Diagnostic{Name: marker, Phase: "verification"}); err != nil {
		t.Fatal(err)
	}
}
