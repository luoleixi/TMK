package runtimeconfig

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

const (
	EnvProduction = "production"
	EnvTest       = "test"

	productionBackendBaseURL = "https://117.72.159.185/tmk-production"
	testBackendBaseURL       = "https://117.72.159.185/tmk-test"
)

func RuntimeEnvironment() string {
	if productionBuild {
		return EnvProduction
	}
	for _, key := range []string{"TMK_ENV", "APP_ENV", "GO_ENV"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return NormalizeEnvironment(value)
		}
	}
	return defaultRuntimeEnvironment
}

func NormalizeEnvironment(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "prod", "production", "release", "online":
		return EnvProduction
	case "test", "testing", "dev", "development", "local":
		return EnvTest
	default:
		return value
	}
}

func BackendBaseURL() string {
	if productionBuild {
		return productionBackendBaseURL
	}
	if value := strings.TrimSpace(os.Getenv("TMK_BACKEND_URL")); value != "" {
		return strings.TrimRight(value, "/")
	}
	if RuntimeEnvironment() == EnvProduction {
		return productionBackendBaseURL
	}
	return testBackendBaseURL
}

func BackendAPIURL() string {
	base := strings.TrimRight(BackendBaseURL(), "/")
	if strings.HasSuffix(base, "/api/v1") {
		return base
	}
	return base + "/api/v1"
}

func BackendWebSocketURL(path string, query url.Values) (string, error) {
	u, err := url.Parse(BackendAPIURL() + path)
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
