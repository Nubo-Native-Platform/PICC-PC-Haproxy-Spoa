package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestIsBlank(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"", true},
		{"   ", true},
		{"\t\n\r", true},
		{"a", false},
		{"  hello  ", false},
	}

	for _, tt := range tests {
		got := IsBlank(tt.input)
		if got != tt.expected {
			t.Errorf("IsBlank(%q) = %v, expected %v", tt.input, got, tt.expected)
		}
	}
}

func TestIsBlankOrNil(t *testing.T) {
	if !IsBlankOrNil(nil) {
		t.Errorf("IsBlankOrNil(nil) should be true")
	}

	blank := "  "
	if !IsBlankOrNil(&blank) {
		t.Errorf("IsBlankOrNil('  ') should be true")
	}

	valid := "valid"
	if IsBlankOrNil(&valid) {
		t.Errorf("IsBlankOrNil('valid') should be false")
	}
}

func TestParseStringSet(t *testing.T) {
	raw := " example.com, test.org , SUB.DOMAIN.NET, "
	set := parseStringSet(raw)

	if len(set) != 3 {
		t.Fatalf("expected 3 items in set, got %d", len(set))
	}

	for _, host := range []string{"example.com", "test.org", "sub.domain.net"} {
		if _, ok := set[host]; !ok {
			t.Errorf("expected %s to be in set", host)
		}
	}

	emptySet := parseStringSet("")
	if len(emptySet) != 0 {
		t.Errorf("expected empty set, got %d items", len(emptySet))
	}
}

func TestIsHostBypassed(t *testing.T) {
	bypass := map[string]struct{}{
		"auth.example.com": {},
		"login.org":        {},
	}

	tests := []struct {
		host     string
		expected bool
	}{
		{"auth.example.com", true},
		{"AUTH.EXAMPLE.COM", true},
		{"auth.example.com:443", true},
		{"other.example.com", false},
		{"", false},
	}

	for _, tt := range tests {
		got := isHostBypassed(tt.host, bypass)
		if got != tt.expected {
			t.Errorf("isHostBypassed(%q) = %v, expected %v", tt.host, got, tt.expected)
		}
	}
}

func TestIsEnvBypassed(t *testing.T) {
	bypass := map[string]struct{}{
		"master": {},
		"admin":  {},
	}

	if !isEnvBypassed("MASTER", bypass) {
		t.Errorf("expected MASTER to be bypassed")
	}
	if !isEnvBypassed("admin", bypass) {
		t.Errorf("expected admin to be bypassed")
	}
	if isEnvBypassed("dev", bypass) {
		t.Errorf("expected dev to NOT be bypassed")
	}
}

func TestLoadConfig(t *testing.T) {
	// Set custom environment variables
	os.Setenv("SPOA_PORT", "9999")
	os.Setenv("HEALTH_PORT", "8888")
	os.Setenv("AUTH_SERVICE_URL", "http://test-service/verify")
	os.Setenv("BYPASS_HOSTS", "test1.com,test2.com")
	os.Setenv("BYPASS_ENV_IDS", "PROD,MASTER")
	os.Setenv("AUTH_TIMEOUT_SECONDS", "10")
	os.Setenv("LOG_LEVEL", "DEBUG")

	defer func() {
		os.Unsetenv("SPOA_PORT")
		os.Unsetenv("HEALTH_PORT")
		os.Unsetenv("AUTH_SERVICE_URL")
		os.Unsetenv("BYPASS_HOSTS")
		os.Unsetenv("BYPASS_ENV_IDS")
		os.Unsetenv("AUTH_TIMEOUT_SECONDS")
		os.Unsetenv("LOG_LEVEL")
	}()

	cfg := LoadConfig()

	if cfg.SpoaPort != ":9999" {
		t.Errorf("expected SpoaPort :9999, got %s", cfg.SpoaPort)
	}
	if cfg.HealthPort != ":8888" {
		t.Errorf("expected HealthPort :8888, got %s", cfg.HealthPort)
	}
	if cfg.AuthServiceURL != "http://test-service/verify" {
		t.Errorf("expected AuthServiceURL http://test-service/verify, got %s", cfg.AuthServiceURL)
	}
	if len(cfg.BypassHosts) != 2 {
		t.Errorf("expected 2 bypass hosts, got %d", len(cfg.BypassHosts))
	}
	if len(cfg.BypassEnvIDs) != 2 {
		t.Errorf("expected 2 bypass env IDs, got %d", len(cfg.BypassEnvIDs))
	}
	if cfg.AuthTimeoutSeconds != 10 {
		t.Errorf("expected AuthTimeoutSeconds 10, got %d", cfg.AuthTimeoutSeconds)
	}
	if cfg.LogLevel != "DEBUG" {
		t.Errorf("expected LogLevel DEBUG, got %s", cfg.LogLevel)
	}
}

func TestCallAuthServiceSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		var req AuthRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(AuthResponse{Allowed: true})
	}))
	defer ts.Close()

	appConfig = &Config{
		AuthTimeoutSeconds: 5,
		LogLevel:           "DEBUG",
	}
	httpClient = ts.Client()

	resp, err := callAuthService(ts.URL, AuthRequest{
		UserID:  "test-user",
		EnvCode: "test-env",
		URL:     "http://example.com/api",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Allowed {
		t.Errorf("expected Allowed true, got false")
	}
}

func TestCallAuthServiceDenied(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(AuthResponse{Allowed: false})
	}))
	defer ts.Close()

	appConfig = &Config{
		AuthTimeoutSeconds: 5,
		LogLevel:           "INFO",
	}
	httpClient = ts.Client()

	resp, err := callAuthService(ts.URL, AuthRequest{
		UserID:  "unauthorized-user",
		EnvCode: "test-env",
		URL:     "http://example.com/api",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Allowed {
		t.Errorf("expected Allowed false, got true")
	}
}

func TestCallAuthServiceErrorStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	appConfig = &Config{
		AuthTimeoutSeconds: 5,
		LogLevel:           "INFO",
	}
	httpClient = ts.Client()

	_, err := callAuthService(ts.URL, AuthRequest{
		UserID:  "test-user",
		EnvCode: "test-env",
		URL:     "http://example.com/api",
	})

	if err == nil {
		t.Fatalf("expected error on HTTP 500, got nil")
	}
}

func TestHealthEndpoints(t *testing.T) {
	srv := startHealthServer(":18080")
	defer srv.Close()

	time.Sleep(50 * time.Millisecond)

	for _, path := range []string{"/healthz", "/readyz", "/livez"} {
		resp, err := http.Get("http://localhost:18080" + path)
		if err != nil {
			t.Fatalf("failed to GET %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200 for %s, got %d", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
}
