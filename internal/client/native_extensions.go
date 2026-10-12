package client

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

type nativeExtensionAsset struct {
	embeddedPath string
	targetPath   string
	marker       string
}

func (installer hookInstaller) installPiOMPExtensions() error {
	assets := []nativeExtensionAsset{
		{
			embeddedPath: "hookassets/pi-extension.ts",
			targetPath:   filepath.Join(installer.repoRoot, ".pi", "extensions", hookProductPrefix, "index.ts"),
			marker:       "// agent-fitness-functions-pi-extension: managed by agent-fitness-functions client install-hooks",
		},
		{
			embeddedPath: "hookassets/native-checker.mjs",
			targetPath:   filepath.Join(installer.repoRoot, ".pi", "extensions", hookProductPrefix, "native-checker.mjs"),
			marker:       "// agent-fitness-functions-native-checker: managed by agent-fitness-functions client install-hooks",
		},
		{
			embeddedPath: "hookassets/omp-extension.ts",
			targetPath:   filepath.Join(installer.repoRoot, ".omp", "extensions", hookProductPrefix, "index.ts"),
			marker:       "// agent-fitness-functions-omp-extension: managed by agent-fitness-functions client install-hooks",
		},
		{
			embeddedPath: "hookassets/native-checker.mjs",
			targetPath:   filepath.Join(installer.repoRoot, ".omp", "extensions", hookProductPrefix, "native-checker.mjs"),
			marker:       "// agent-fitness-functions-native-checker: managed by agent-fitness-functions client install-hooks",
		},
	}

	for _, asset := range assets {
		content, err := os.ReadFile(asset.targetPath)
		if err == nil {
			if !bytes.Contains(content, []byte(asset.marker)) {
				return fmt.Errorf("refusing to overwrite unmanaged native extension %s: missing product marker", asset.targetPath)
			}
			continue
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("checking native extension %s: %w", asset.targetPath, err)
		}
	}

	for _, asset := range assets {
		if err := os.MkdirAll(filepath.Dir(asset.targetPath), 0o755); err != nil {
			return fmt.Errorf("creating native extension directory for %s: %w", asset.targetPath, err)
		}
		if err := installer.writeEmbeddedFile(asset.embeddedPath, asset.targetPath, 0o644); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(installer.stdout, "installed %s\n", asset.targetPath)
	}
	_, _ = fmt.Fprintln(installer.stdout, "OMP extension uses runtime-only strict replace mode with fuzzy matching disabled; restart or reload OMP sessions after installation")
	return nil
}
