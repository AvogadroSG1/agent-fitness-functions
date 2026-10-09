//go:build integration

package server

import (
	"fmt"
	"os"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/roslyntest"
)

func TestMain(m *testing.M) {
	code := m.Run()
	if err := roslyntest.Cleanup(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}
