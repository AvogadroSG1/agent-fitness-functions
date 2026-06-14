// Package client owns the commit-time validation client for stack-fitness-functions.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/poconnor/calm-poc/internal/fitness"
	"github.com/poconnor/calm-poc/internal/sarif"
)

// RunCheck validates one file by posting a validation request to the daemon.
func RunCheck(args []string, stdout io.Writer, httpClient *http.Client, starter func(string) error) error {
	flags := flag.NewFlagSet("client validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	addr := flags.String("addr", "http://localhost:7890", "daemon base URL")
	file := flags.String("file", "", "file path being checked")
	repo := flags.String("repo", "", "repository root")
	content := flags.String("content", "", "proposed file content")
	contentFile := flags.String("content-file", "", "path to proposed file content")
	language := flags.String("language", "", "source language")
	staged := flags.Bool("staged", false, "read content from git staged state")
	format := flags.String("format", "json", "output format: json or sarif")
	clientCert := flags.String("client-cert", "", "mTLS client certificate path")
	clientKey := flags.String("client-key", "", "mTLS client private key path")
	clientCA := flags.String("client-ca", "", "server CA bundle path")
	if err := flags.Parse(args); err != nil {
		return usageError{err: err}
	}
	if *file == "" || *repo == "" {
		return usageError{err: errors.New("client validate requires --file and --repo")}
	}
	validFormats := map[string]bool{"json": true, "sarif": true}
	if !validFormats[*format] {
		return usageError{err: fmt.Errorf("unsupported format %q: use json or sarif", *format)}
	}
	configuredClient, err := configureTLS(httpClient, *clientCert, *clientKey, *clientCA)
	if err != nil {
		return err
	}
	if err := ensureDaemon(configuredClient, *addr, starter); err != nil {
		return err
	}
	proposedContent, err := resolveContent(*repo, *file, *content, *contentFile, *staged)
	if err != nil {
		return err
	}
	body, err := postCheck(context.Background(), configuredClient, *addr, fitness.ValidationRequest{
		Repo:            *repo,
		File:            *file,
		ProposedContent: proposedContent,
		Language:        *language,
	})
	if err != nil {
		return err
	}
	return writeValidationResult(stdout, *format, *repo, body)
}

func ensureDaemon(httpClient *http.Client, addr string, starter func(string) error) error {
	if isHealthy(httpClient, addr) {
		return nil
	}
	if err := starter(addr); err != nil {
		return fmt.Errorf("starting daemon: %w", err)
	}
	return waitHealthy(httpClient, addr, 500*time.Millisecond)
}

func postCheck(ctx context.Context, httpClient *http.Client, addr string, request fitness.ValidationRequest) ([]byte, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encoding check request: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(addr, "/")+"/check", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building check request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("posting check request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		message := strings.TrimSpace(string(errBody))
		if message == "" {
			return nil, fmt.Errorf("check failed with HTTP %d", response.StatusCode)
		}
		return nil, fmt.Errorf("check failed with HTTP %d: %s", response.StatusCode, message)
	}
	return io.ReadAll(io.LimitReader(response.Body, 10<<20))
}

func configureTLS(base *http.Client, certPath, keyPath, caPath string) (*http.Client, error) {
	if certPath == "" && keyPath == "" && caPath == "" {
		return base, nil
	}
	if (certPath == "") != (keyPath == "") {
		return nil, usageError{err: errors.New("client validate requires --client-cert and --client-key together")}
	}
	configured := cloneHTTPClient(base)
	transport := cloneTransport(configured)
	tlsConfig, err := buildTLSConfig(transport.TLSClientConfig, certPath, keyPath, caPath)
	if err != nil {
		return nil, err
	}
	transport.TLSClientConfig = tlsConfig
	configured.Transport = transport
	return configured, nil
}

func cloneTransport(httpClient *http.Client) *http.Transport {
	base := http.DefaultTransport.(*http.Transport)
	if httpClient.Transport != nil {
		if transport, ok := httpClient.Transport.(*http.Transport); ok {
			base = transport
		}
	}
	return base.Clone()
}

func buildTLSConfig(existing *tls.Config, certPath, keyPath, caPath string) (*tls.Config, error) {
	tlsConfig := cloneTLSConfig(existing)
	if certPath != "" {
		certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, fmt.Errorf("loading client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	if caPath != "" {
		caContent, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("reading client CA bundle: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caContent) {
			return nil, errors.New("parsing client CA bundle")
		}
		tlsConfig.RootCAs = pool
	}
	return tlsConfig, nil
}

func cloneTLSConfig(existing *tls.Config) *tls.Config {
	if existing != nil {
		return existing.Clone()
	}
	return &tls.Config{MinVersion: tls.VersionTLS12}
}

func cloneHTTPClient(base *http.Client) *http.Client {
	if base == nil {
		return &http.Client{Timeout: 2 * time.Second}
	}
	clone := *base
	return &clone
}

func writeValidationResult(stdout io.Writer, format, repo string, body []byte) error {
	if format != "sarif" {
		_, err := stdout.Write(body)
		return err
	}
	var result fitness.ValidationResult
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decoding check response: %w", err)
	}
	return json.NewEncoder(stdout).Encode(sarif.Convert(result, repo))
}

func resolveContent(repo, file, explicitContent, contentFile string, staged bool) (string, error) {
	if contentFile != "" {
		output, err := os.ReadFile(contentFile)
		if err != nil {
			return "", fmt.Errorf("reading content file %s: %w", contentFile, err)
		}
		return string(output), nil
	}
	if explicitContent != "" {
		return explicitContent, nil
	}
	if staged {
		return resolveContentFromGit(repo, file)
	}
	return resolveContentFromDisk(repo, file)
}

func resolveContentFromGit(repo, file string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", "-C", repo, "show", ":"+file)
	output, err := command.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			return "", fmt.Errorf("reading staged content for %s: %w", file, err)
		}
		return "", fmt.Errorf("reading staged content for %s: %w: %s", file, err, detail)
	}
	return string(output), nil
}

func resolveContentFromDisk(repo, file string) (string, error) {
	path := file
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo, file)
	}
	output, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(output), nil
}

func isHealthy(httpClient *http.Client, addr string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(addr, "/")+"/health", nil)
	if err != nil {
		return false
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func waitHealthy(httpClient *http.Client, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isHealthy(httpClient, addr) {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("daemon at %s did not become healthy within %s", addr, timeout)
}

// StartDaemon starts a detached daemon process using the current executable.
func StartDaemon(addr string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	listenAddr := strings.TrimPrefix(strings.TrimPrefix(addr, "http://"), "https://")
	command := exec.Command(executable, "server", "start", "--addr", listenAddr)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

type usageError struct {
	err error
}

func (e usageError) Error() string {
	return e.err.Error()
}

func (e usageError) Unwrap() error {
	return e.err
}

// IsUsageError reports whether err is a client usage error.
func IsUsageError(err error) bool {
	var target usageError
	return errors.As(err, &target)
}
