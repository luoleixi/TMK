package runtimeconfig

import "testing"

func TestBackendConfigurationMatchesBuildType(t *testing.T) {
	t.Setenv("TMK_ENV", EnvTest)
	t.Setenv("APP_ENV", EnvTest)
	t.Setenv("GO_ENV", EnvTest)
	t.Setenv("TMK_BACKEND_URL", "http://127.0.0.1:9999")

	if productionBuild {
		if got := RuntimeEnvironment(); got != EnvProduction {
			t.Fatalf("production runtime environment = %q, want %q", got, EnvProduction)
		}
		if got := BackendBaseURL(); got != productionBackendBaseURL {
			t.Fatalf("production backend = %q, want %q", got, productionBackendBaseURL)
		}
		return
	}

	if got := RuntimeEnvironment(); got != EnvTest {
		t.Fatalf("development runtime environment = %q, want %q", got, EnvTest)
	}
	if got := BackendBaseURL(); got != "http://127.0.0.1:9999" {
		t.Fatalf("development backend override = %q", got)
	}
}
