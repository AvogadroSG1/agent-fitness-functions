package main

import (
	"context"
	"io"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/client"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/historyipc"
)

func TestUninstallUsageNeverProbesWriter(t *testing.T) {
	runtime := client.HistoryRuntime{Request: func(context.Context, string, string) (historyipc.Message, error) {
		t.Fatal("unconfirmed uninstall touched writer")
		return historyipc.Message{}, nil
	}}
	for _, args := range [][]string{{"uninstall"}, {"uninstall", "--yes", "--unknown"}, {"uninstall", "--yes", "extra"}} {
		if code := runWithDependencies(args, io.Discard, io.Discard, nil, nil, &runtime); code != 2 {
			t.Fatalf("args %v exit=%d", args, code)
		}
	}
}

func TestHistoryWriterRejectsArgumentsBeforeStarting(t *testing.T) {
	if code := runInternalCommand([]string{"history-writer", "extra"}, io.Discard, io.Discard); code != 2 {
		t.Fatalf("exit=%d", code)
	}
}

func TestCertificatesOnlyNeverProbesWriter(t *testing.T) {
	t.Setenv("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR", t.TempDir()+"/certs")
	runtime := client.HistoryRuntime{Request: func(context.Context, string, string) (historyipc.Message, error) {
		t.Fatal("certificates-only touched history")
		return historyipc.Message{}, nil
	}}
	if code := runWithDependencies([]string{"client", "onboard", "--certificates-only"}, io.Discard, io.Discard, nil, nil, &runtime); code != 0 {
		t.Fatalf("exit=%d", code)
	}
}
