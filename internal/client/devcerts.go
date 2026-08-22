package client

import "github.com/AvogadroSG1/agent-fitness-functions/internal/devcerts"

const (
	devCACertName     = "ca.crt"
	devServerCertName = "server.crt"
	devServerKeyName  = "server.key"
	devClientCertName = "client.crt"
	devClientKeyName  = "client.key"

	devCACommonName     = "agent-fitness-functions-dev-ca"
	devServerCommonName = "localhost"
	devClientCommonName = "dev-hook-pool"
)

var devCertFileNames = []string{devCACertName, devServerCertName, devServerKeyName, devClientCertName, devClientKeyName}

// EnsureDevCerts publishes the managed local development certificate set.
func EnsureDevCerts(certDir string) error {
	return devcerts.Publish(certDir, false)
}

func ensureDevCerts(certDir string, force bool) error {
	return devcerts.Publish(certDir, force)
}
