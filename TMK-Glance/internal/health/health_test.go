package health

import "testing"

func TestReadinessIsIndependentFromOptionalServices(t *testing.T) {
	SetReady(false)
	t.Cleanup(func() { SetReady(false) })
	if Ready() {
		t.Fatal("expected application to start not ready")
	}
	SetReady(true)
	if !Ready() {
		t.Fatal("expected application to become ready")
	}
	ready, status, services, states := Snapshot()
	if !ready || status != "ok" || len(services) == 0 || states["asr"] != "unknown" {
		t.Fatalf("unexpected snapshot: ready=%v status=%q services=%v states=%v", ready, status, services, states)
	}
}

func TestFailedAndPanickingChecksDegradeWithoutCrashing(t *testing.T) {
	Register("failed-test", func() bool { return false })
	Register("panic-test", func() bool { panic("boom") })
	SetReady(true)
	t.Cleanup(func() {
		Register("failed-test", nil)
		Register("panic-test", nil)
		SetReady(false)
	})
	_, status, _, states := Snapshot()
	if status != "degraded" || states["failed-test"] != "unavailable" || states["panic-test"] != "unavailable" {
		t.Fatalf("unexpected degraded snapshot: status=%q states=%v", status, states)
	}
}
