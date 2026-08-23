package installer

// Pinned managed-tool versions (ADR-0005 "Managed Tool Components and
// Integrity"). This is the installer's single manifest: these constants MUST
// agree with the Dockerfile's pins, requirements.lock's radon pin, and
// tools/calm-runtime/package-lock.json's @finos/calm-cli pin. Any divergence
// is caught by TestManagedToolPinsAgreeAcrossProvisioningSources
// (pins_drift_test.go) so the two provisioning implementations (this
// installer and the Dockerfile) cannot silently drift apart.
const (
	// CALMCLIVersion is the pinned FINOS CALM CLI version, provisioned via
	// npm ci against the committed tools/calm-runtime/package-lock.json.
	CALMCLIVersion = "1.40.0"
	// RadonVersion is the pinned radon version, installed into the managed
	// Python venv via `pip install --require-hashes -r requirements.lock`.
	RadonVersion = "6.0.1"
	// DotnetSDKVersion is the pinned .NET SDK version used to build the
	// release archive's self-contained Roslyn analyzer, matching the
	// Dockerfile's dotnet-build stage.
	DotnetSDKVersion = "8.0.301"
)
