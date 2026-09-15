package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	spoe "github.com/criteo/haproxy-spoe-go"
	"github.com/tidwall/gjson"
)

// Config represents runtime configuration loaded from environment variables
type Config struct {
	SpoaPort           string
	HealthPort         string
	AuthServiceURL     string
	BypassHosts        map[string]struct{}
	BypassEnvIDs       map[string]struct{}
	AuthTimeoutSeconds int
	LogLevel           string
}

// Global runtime configuration and HTTP client
var (
	appConfig  *Config
	httpClient *http.Client
)

// LoadConfig initializes the configuration from environment variables with safe defaults
func LoadConfig() *Config {
	spoaPort := getEnv("SPOA_PORT", ":9000")
	if !strings.HasPrefix(spoaPort, ":") {
		spoaPort = ":" + spoaPort
	}

	healthPort := getEnv("HEALTH_PORT", ":8080")
	if !strings.HasPrefix(healthPort, ":") {
		healthPort = ":" + healthPort
	}

	authURL := getEnv("AUTH_SERVICE_URL", "http://localhost:8080/api/v1/auth/access")

	// Comma-separated list of hostnames allowed to bypass authentication
	bypassHostsRaw := getEnv("BYPASS_HOSTS", "")
	bypassHosts := parseStringSet(bypassHostsRaw)

	// Comma-separated list of environment IDs allowed to bypass authentication
	bypassEnvRaw := getEnv("BYPASS_ENV_IDS", "MASTER")
	bypassEnvIDs := parseStringSet(bypassEnvRaw)

	timeoutSecStr := getEnv("AUTH_TIMEOUT_SECONDS", "5")
	timeoutSec, err := strconv.Atoi(timeoutSecStr)
	if err != nil || timeoutSec <= 0 {
		timeoutSec = 5
	}

	logLevel := strings.ToUpper(getEnv("LOG_LEVEL", "INFO"))

	return &Config{
		SpoaPort:           spoaPort,
		HealthPort:         healthPort,
		AuthServiceURL:     authURL,
		BypassHosts:        bypassHosts,
		BypassEnvIDs:       bypassEnvIDs,
		AuthTimeoutSeconds: timeoutSec,
		LogLevel:           logLevel,
	}
}

func parseStringSet(raw string) map[string]struct{} {
	set := make(map[string]struct{})
	if strings.TrimSpace(raw) == "" {
		return set
	}
	for _, item := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			set[strings.ToLower(trimmed)] = struct{}{}
		}
	}
	return set
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && strings.TrimSpace(val) != "" {
		return strings.TrimSpace(val)
	}
	return defaultVal
}

// AuthRequest represents the authorization verification payload sent to the upstream service
type AuthRequest struct {
	UserID  string `json:"userId"`
	EnvCode string `json:"envCode"`
	EnvID   string `json:"envId,omitempty"`
	URL     string `json:"url"`
}

// AuthResponse represents the authorization decision received from the upstream service
type AuthResponse struct {
	Allowed bool `json:"allowed"`
}

// extractMessageArgs safely parses all SPOE message arguments into a key-value map
func extractMessageArgs(msg *spoe.Message) map[string]string {
	args := make(map[string]string)
	for msg.Args.Next() {
		arg := msg.Args.Arg
		key := strings.ToLower(strings.TrimSpace(arg.Name))
		if arg.Value == nil {
			args[key] = ""
		} else {
			args[key] = fmt.Sprintf("%v", arg.Value)
		}
	}
	return args
}

func callAuthService(apiURL string, payload AuthRequest) (*AuthResponse, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal auth request: %w", err)
	}

	if appConfig.LogLevel == "DEBUG" {
		log.Printf("[DEBUG] SPOE Request to Auth Service: %s", string(b))
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(appConfig.AuthTimeoutSeconds)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewBuffer(b))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "HAProxy-SPOA-Agent/1.0")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth service call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("auth service returned non-2xx status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read auth service response body: %w", err)
	}

	var authResp AuthResponse
	if err := json.Unmarshal(body, &authResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal auth service response: %w", err)
	}

	return &authResp, nil
}

func handleSPOEMessage(msg *spoe.Message) ([]spoe.Action, error) {
	args := extractMessageArgs(msg)

	hostURL := args["url"]
	userID := args["userid"]
	envRAW := args["envraw"]

	// Check if host is configured to bypass authorization
	if isHostBypassed(hostURL, appConfig.BypassHosts) {
		if appConfig.LogLevel == "DEBUG" {
			log.Printf("[DEBUG] Host %s matches bypass list, granting access", hostURL)
		}
		return allowAction(), nil
	}

	// Fail-safe check: deny if any required variable is empty or nil
	if IsBlank(hostURL) || IsBlank(userID) || IsBlank(envRAW) {
		if appConfig.LogLevel == "DEBUG" {
			log.Printf("[DEBUG] Missing required parameter (url='%s', userid='%s', envraw='%s'); denying access", hostURL, userID, envRAW)
		}
		return denyAction(), nil
	}

	// URL-decode the environment raw parameter safely without crashing on invalid input
	decoded, err := url.QueryUnescape(envRAW)
	if err != nil {
		log.Printf("[WARN] Failed to unescape envraw parameter '%s': %v; denying access", envRAW, err)
		return denyAction(), nil
	}

	// Extract environment identifier from decoded JSON payload
	envID := gjson.Get(decoded, "envId").String()
	if envID == "" {
		envID = gjson.Get(decoded, "envCode").String()
	}

	// Check if environment ID is configured to bypass authorization
	if isEnvBypassed(envID, appConfig.BypassEnvIDs) {
		if appConfig.LogLevel == "DEBUG" {
			log.Printf("[DEBUG] Environment ID '%s' matches bypass list, granting access", envID)
		}
		return allowAction(), nil
	}

	authReq := AuthRequest{
		UserID:  userID,
		EnvCode: envID,
		EnvID:   envID,
		URL:     hostURL,
	}

	resp, err := callAuthService(appConfig.AuthServiceURL, authReq)
	if err != nil {
		log.Printf("[ERROR] Auth service verification failed for user='%s', env='%s', url='%s': %v", userID, envID, hostURL, err)
		// Cloud-native zero-trust principle: fail closed (deny)
		return denyAction(), nil
	}

	if appConfig.LogLevel == "DEBUG" {
		log.Printf("[DEBUG] Auth decision for user='%s', url='%s': allowed=%v", userID, hostURL, resp.Allowed)
	}

	return setAllowAction(resp.Allowed), nil
}

func isHostBypassed(host string, bypassMap map[string]struct{}) bool {
	if len(bypassMap) == 0 || host == "" {
		return false
	}
	cleanHost := strings.ToLower(strings.TrimSpace(host))
	// Strip port if present
	if h, _, err := net.SplitHostPort(cleanHost); err == nil {
		cleanHost = h
	}
	_, ok := bypassMap[cleanHost]
	return ok
}

func isEnvBypassed(envID string, bypassMap map[string]struct{}) bool {
	if len(bypassMap) == 0 || envID == "" {
		return false
	}
	_, ok := bypassMap[strings.ToLower(strings.TrimSpace(envID))]
	return ok
}

func allowAction() []spoe.Action {
	return setAllowAction(true)
}

func denyAction() []spoe.Action {
	return setAllowAction(false)
}

func setAllowAction(allowed bool) []spoe.Action {
	return []spoe.Action{
		spoe.ActionSetVar{
			Name:  "allow",
			Scope: spoe.VarScopeSession,
			Value: allowed,
		},
	}
}

// startHealthServer initializes an HTTP server for Kubernetes liveness and readiness probes
func startHealthServer(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"UP"}`))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"READY"}`))
	})
	mux.HandleFunc("/livez", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ALIVE"}`))
	})

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("[INFO] Health probe HTTP server listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[ERROR] Health HTTP server failed: %v", err)
		}
	}()

	return srv
}

func main() {
	appConfig = LoadConfig()

	// Configure hardened HTTP client with connection pooling and timeouts
	httpClient = &http.Client{
		Timeout: time.Duration(appConfig.AuthTimeoutSeconds) * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 50,
			IdleConnTimeout:     90 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}

	// Start health probe server for Kubernetes
	healthSrv := startHealthServer(appConfig.HealthPort)

	// Create HAProxy SPOE agent
	agent := spoe.New(func(msgs *spoe.MessageIterator) ([]spoe.Action, error) {
		for msgs.Next() {
			msg := msgs.Message
			return handleSPOEMessage(&msg)
		}
		return nil, nil
	})

	listener, err := net.Listen("tcp", appConfig.SpoaPort)
	if err != nil {
		log.Fatalf("[FATAL] Failed to listen on SPOA port %s: %v", appConfig.SpoaPort, err)
	}

	log.Printf("[INFO] HAProxy SPOA agent listening on TCP %s (Auth URL: %s)", appConfig.SpoaPort, appConfig.AuthServiceURL)

	// Handle graceful shutdown on OS signals (SIGINT, SIGTERM)
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-shutdownChan
		log.Println("[INFO] Graceful shutdown initiated...")

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = healthSrv.Shutdown(ctx)

		_ = listener.Close()
		log.Println("[INFO] HAProxy SPOA agent stopped cleanly")
		os.Exit(0)
	}()

	if err := agent.Serve(listener); err != nil {
		select {
		case <-shutdownChan:
			// Normal shutdown
		default:
			log.Fatalf("[FATAL] SPOA agent server error: %v", err)
		}
	}
}

// IsBlank checks whether a string is empty or contains only whitespace
func IsBlank(s string) bool {
	return strings.TrimSpace(s) == ""
}

// IsBlankOrNil checks whether a string pointer is nil or points to a blank string
func IsBlankOrNil(s *string) bool {
	if s == nil {
		return true
	}
	return IsBlank(*s)
}
