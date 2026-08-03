package main

import "testing"

func TestBackendConfigurationMatchesBuildType(t *testing.T) {
	t.Setenv("TMK_ENV", envTest)
	t.Setenv("APP_ENV", envTest)
	t.Setenv("GO_ENV", envTest)
	t.Setenv("TMK_BACKEND_URL", "http://127.0.0.1:9999")

	if productionBuild {
		if got := runtimeEnvironment(); got != envProduction {
			t.Fatalf("production runtime environment = %q, want %q", got, envProduction)
		}
		if got := backendBaseURL(); got != productionBackendBaseURL {
			t.Fatalf("production backend = %q, want %q", got, productionBackendBaseURL)
		}
		return
	}

	if got := runtimeEnvironment(); got != envTest {
		t.Fatalf("development runtime environment = %q, want %q", got, envTest)
	}
	if got := backendBaseURL(); got != "http://127.0.0.1:9999" {
		t.Fatalf("development backend override = %q", got)
	}
}
