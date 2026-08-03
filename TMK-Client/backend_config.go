package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

const (
	envProduction = "production"
	envTest       = "test"

	productionBackendBaseURL = "https://117.72.159.185/tmk-production"
	testBackendBaseURL       = "https://117.72.159.185/tmk-test"
)

func runtimeEnvironment() string {
	if productionBuild {
		return envProduction
	}
	for _, key := range []string{"TMK_ENV", "APP_ENV", "GO_ENV"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return normalizeEnvironment(value)
		}
	}
	return defaultRuntimeEnvironment
}

func normalizeEnvironment(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "prod", "production", "release", "online":
		return envProduction
	case "test", "testing", "dev", "development", "local":
		return envTest
	default:
		return value
	}
}

func backendBaseURL() string {
	if productionBuild {
		return productionBackendBaseURL
	}
	if value := strings.TrimSpace(os.Getenv("TMK_BACKEND_URL")); value != "" {
		return strings.TrimRight(value, "/")
	}
	if runtimeEnvironment() == envProduction {
		return productionBackendBaseURL
	}
	return testBackendBaseURL
}

func backendAPIURL() string {
	base := strings.TrimRight(backendBaseURL(), "/")
	if strings.HasSuffix(base, "/api/v1") {
		return base
	}
	return base + "/api/v1"
}

func backendWebSocketURL(path string, query url.Values) (string, error) {
	u, err := url.Parse(backendAPIURL() + path)
	if err != nil {
		return "", fmt.Errorf("parse backend url: %w", err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported backend scheme %q", u.Scheme)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}
