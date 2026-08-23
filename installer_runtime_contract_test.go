package calm_poc_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// calm-poc-phk.2 slices 4-8 (ADR-0005): the release archive ships a
// self-contained Roslyn analyzer that never touches host dotnet; install
// provisions hash-verified CALM and Python runtimes under per-tool
// versioned current pointers; doctor names corrupt components and
// --repair re-provisions only what is broken.

func buildRelease(t *testing.T) (archivePath, checksumsPath string) {
	t.Helper()
	packageDir := t.TempDir()
	command := exec.Command("bash", filepath.Join("scripts", "package-release.sh"), "--output", packageDir)
	command.Env = append(os.Environ(), "DOTNET_ROOT="+os.Getenv("HOME")+"/.dotnet")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("package-release.sh: %v\n%s", err, output)
	}
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tar.gz") {
			archivePath = filepath.Join(packageDir, entry.Name())
		}
	}
	checksumsPath = filepath.Join(packageDir, "SHA256SUMS")
	if archivePath == "" {
		t.Fatal("packaging produced no archive")
	}
	return archivePath, checksumsPath
}

func TestReleaseArchiveShipsSelfContainedRoslynAnalyzer(t *testing.T) {
	archivePath, _ := buildRelease(t)

	extractDir := t.TempDir()
	if output, err := exec.Command("tar", "-xzf", archivePath, "-C", extractDir).CombinedOutput(); err != nil {
		t.Fatalf("extract archive: %v\n%s", err, output)
	}
	var analyzer string
	if err := filepath.WalkDir(extractDir, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.Contains(entry.Name(), "RoslynAnalyzer") {
			analyzer = path
		}
		return err
	}); err != nil {
		t.Fatalf("walk extracted archive: %v", err)
	}
	if analyzer == "" {
		t.Fatal("release archive does not ship a self-contained Roslyn analyzer executable")
	}

	// The analyzer must run with no host dotnet reachable and no
	// DOTNET_ROOT: self-contained means it bundles its own runtime.
	command := exec.Command(analyzer)
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()}
	output, _ := command.CombinedOutput()
	if strings.Contains(string(output), "No .NET SDKs were found") ||
		strings.Contains(string(output), "hostfxr") {
		t.Fatalf("analyzer depends on a host .NET installation:\n%s", output)
	}
}

func TestInstallProvisionsManagedCalmAndPythonRuntimes(t *testing.T) {
	archivePath, checksumsPath := buildRelease(t)

	stateHome := t.TempDir()
	env := []string{"XDG_STATE_HOME=" + stateHome}
	command := exec.Command("bash", filepath.Join("scripts", "install.sh"),
		"--archive", archivePath, "--checksums", checksumsPath, "--provision-runtimes")
	command.Env = append(os.Environ(), env...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("install.sh --provision-runtimes: %v\n%s", err, output)
	}

	stateRoot := filepath.Join(stateHome, "agent-fitness-functions")

	calmBinary := filepath.Join(stateRoot, "runtimes", "calm", "current", "node_modules", ".bin", "calm")
	if _, err := os.Stat(calmBinary); err != nil {
		t.Errorf("managed CALM CLI missing: %v", err)
	}
	calmVersions, err := os.ReadDir(filepath.Join(stateRoot, "runtimes", "calm", "versions"))
	if err != nil || len(calmVersions) != 1 {
		t.Fatalf("calm runtime versions = %v (%v), want exactly one", calmVersions, err)
	}
	if _, err := os.Stat(filepath.Join(stateRoot, "runtimes", "calm", "versions", calmVersions[0].Name(), ".verified")); err != nil {
		t.Errorf("calm runtime lacks .verified sentinel: %v", err)
	}

	radon := filepath.Join(stateRoot, "runtimes", "python", "current", "bin", "radon")
	radonOutput, err := exec.Command(radon, "--version").CombinedOutput()
	if err != nil || !strings.Contains(string(radonOutput), "6.0.1") {
		t.Errorf("managed radon --version = %q (%v), want 6.0.1", radonOutput, err)
	}
	pythonBinary := filepath.Join(stateRoot, "runtimes", "python", "current", "bin", "python3")
	pyyamlCheck := exec.Command(pythonBinary, "-c", "import yaml; print(yaml.__version__)")
	pyyamlOutput, err := pyyamlCheck.CombinedOutput()
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(pyyamlOutput)), "6.0") {
		t.Errorf("managed venv pyyaml = %q (%v), want a 6.0.x import", pyyamlOutput, err)
	}
}

func TestDoctorNamesCorruptRuntimeAndRepairFixesOnlyIt(t *testing.T) {
	archivePath, checksumsPath := buildRelease(t)

	stateHome := t.TempDir()
	env := []string{"XDG_STATE_HOME=" + stateHome}
	install := exec.Command("bash", filepath.Join("scripts", "install.sh"),
		"--archive", archivePath, "--checksums", checksumsPath, "--provision-runtimes")
	install.Env = append(os.Environ(), env...)
	if output, err := install.CombinedOutput(); err != nil {
		t.Fatalf("install.sh: %v\n%s", err, output)
	}

	stateRoot := filepath.Join(stateHome, "agent-fitness-functions")
	binary := filepath.Join(stateRoot, "current", "bin", "agent-fitness-functions")

	radon := filepath.Join(stateRoot, "runtimes", "python", "current", "bin", "radon")
	resolved, err := filepath.EvalSymlinks(radon)
	if err != nil {
		t.Fatalf("resolve managed radon: %v", err)
	}
	if err := os.Remove(resolved); err != nil {
		t.Fatalf("corrupt python runtime: %v", err)
	}

	doctor := exec.Command(binary, "runtime", "doctor")
	doctor.Env = append(os.Environ(), env...)
	doctorOutput, doctorErr := doctor.CombinedOutput()
	if doctorErr == nil {
		t.Fatalf("runtime doctor exited zero with a corrupt python runtime:\n%s", doctorOutput)
	}
	if !strings.Contains(string(doctorOutput), "python") {
		t.Fatalf("runtime doctor does not name the corrupt component:\n%s", doctorOutput)
	}

	repair := exec.Command(binary, "runtime", "doctor", "--repair")
	repair.Env = append(os.Environ(), env...)
	if output, err := repair.CombinedOutput(); err != nil {
		t.Fatalf("runtime doctor --repair: %v\n%s", err, output)
	}
	clean := exec.Command(binary, "runtime", "doctor")
	clean.Env = append(os.Environ(), env...)
	if output, err := clean.CombinedOutput(); err != nil {
		t.Fatalf("runtime doctor still failing after repair: %v\n%s", err, output)
	}
	if _, err := exec.Command(radon, "--version").CombinedOutput(); err != nil {
		t.Fatalf("repair did not restore the python runtime: %v", err)
	}
}
