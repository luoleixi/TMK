package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLegacyAdminRoutesAreNotExposed(t *testing.T) {
	app := newTestApplication(t, "admin-boundary")
	for _, path := range []string{"/admin", "/admin/", "/api/v1/admin/users"} {
		request := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		app.Router().ServeHTTP(request, req)
		if request.Code != http.StatusNotFound {
			t.Fatalf("legacy route %s returned %d", path, request.Code)
		}
	}
}
