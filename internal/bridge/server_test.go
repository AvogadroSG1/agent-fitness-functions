package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandlerHealthReturnsOK(t *testing.T) {
	server := httptest.NewServer(NewHandler(nil))
	defer server.Close()

	response, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /health status = %d, want %d", response.StatusCode, http.StatusOK)
	}
}

func TestHandlerCheckAcceptsSchemaAndReturnsPass(t *testing.T) {
	server := httptest.NewServer(NewHandler(nil))
	defer server.Close()

	body := []byte(`{"repo":"/tmp/repo","file":"internal/parser/parser.go","proposed_content":"package parser\n","language":"go"}`)
	response, err := http.Post(server.URL+"/check", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /check failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST /check status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	var checkResponse CheckResponse
	if err := json.NewDecoder(response.Body).Decode(&checkResponse); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if checkResponse.Status != StatusPass {
		t.Fatalf("status = %q, want %q", checkResponse.Status, StatusPass)
	}
}

func TestHandlerShutdownInvokesCallback(t *testing.T) {
	called := false
	server := httptest.NewServer(NewHandler(func() {
		called = true
	}))
	defer server.Close()

	response, err := http.Post(server.URL+"/shutdown", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /shutdown failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST /shutdown status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if !called {
		t.Fatal("shutdown callback was not invoked")
	}
}

func TestServeStartsDaemonAndRespondsToHealth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, "127.0.0.1:7891")
	}()

	client := &http.Client{Timeout: time.Second}
	var response *http.Response
	var err error
	for range 40 {
		response, err = client.Get("http://127.0.0.1:7891/health")
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("daemon did not become healthy: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /health status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("Serve returned %v, want nil or context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop after context cancellation")
	}
}

func TestServeStopsAfterShutdownRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, "127.0.0.1:7892")
	}()

	client := &http.Client{Timeout: time.Second}
	waitForHealth(t, client, "http://127.0.0.1:7892")

	response, err := client.Post("http://127.0.0.1:7892/shutdown", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /shutdown failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST /shutdown status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop after /shutdown")
	}
}

func waitForHealth(t *testing.T, client *http.Client, addr string) *http.Response {
	t.Helper()
	var response *http.Response
	var err error
	for range 40 {
		response, err = client.Get(addr + "/health")
		if err == nil {
			return response
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("daemon did not become healthy: %v", err)
	return nil
}
