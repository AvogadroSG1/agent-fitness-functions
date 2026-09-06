package calm_poc_test

import (
	"os"
	"strings"
	"testing"
)

// S1 red-test contract (calm-poc-vdxf): `make install` is the canonical deploy
// command and must replace the installed binary atomically. On Apple Silicon,
// copying over a running binary in place corrupts the kernel's code-signature
// cache and the next exec is SIGKILLed, so the install recipe must use BSD
// install's safe-copy mode (-S: write to a temporary file, then rename into
// place) for the main binary.
func TestMakefileInstallsBinaryWithSafeCopy(t *testing.T) {
	content, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("reading Makefile: %v", err)
	}
	makefile := string(content)

	if !strings.Contains(makefile, "install -S") {
		t.Error("Makefile install recipe must use `install -S` (safe copy: temp file + atomic rename) for the main binary")
	}
	if strings.Contains(makefile, "install -m 755 $(BIN_DIR)/$(BIN_NAME)") {
		t.Error("Makefile must not install the main binary with a plain in-place `install -m 755` copy")
	}
}
