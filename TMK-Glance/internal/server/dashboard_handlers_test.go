package server

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAdminDashboardGovernanceAndAuditAPIs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newTestApplication(t, "dashboard-api")
	pair := loginTestUser(t, app, "test@example.com", "test-password-123")

	dashboard := requestWithToken(app.Router(), http.MethodGet, "/api/v1/admin/dashboard?days=7", nil, pair.AccessToken)
	if dashboard.Code != http.StatusOK || !bytes.Contains(dashboard.Body.Bytes(), []byte(`"window_days":7`)) ||
		!bytes.Contains(dashboard.Body.Bytes(), []byte(`"evaluations"`)) {
		t.Fatalf("dashboard status=%d body=%s", dashboard.Code, dashboard.Body.String())
	}
	for _, forbidden := range [][]byte{[]byte(`password_hash`), []byte(`source_text`), []byte(`translated_text`)} {
		if bytes.Contains(dashboard.Body.Bytes(), forbidden) {
			t.Fatalf("dashboard leaked field %q: %s", forbidden, dashboard.Body.String())
		}
	}
	invalid := requestWithToken(app.Router(), http.MethodGet, "/api/v1/admin/dashboard?days=365", nil, pair.AccessToken)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid dashboard window status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	governance := requestWithToken(app.Router(), http.MethodGet, "/api/v1/admin/governance/report", nil, pair.AccessToken)
	if governance.Code != http.StatusOK || !bytes.Contains(governance.Body.Bytes(), []byte(`"session_retention_days":180`)) ||
		!bytes.Contains(governance.Body.Bytes(), []byte(`"unreferenced_objects"`)) {
		t.Fatalf("governance status=%d body=%s", governance.Code, governance.Body.String())
	}

	audit := requestWithToken(app.Router(), http.MethodGet, "/api/v1/admin/audit-logs?result=success", nil, pair.AccessToken)
	if audit.Code != http.StatusOK || !bytes.Contains(audit.Body.Bytes(), []byte(`"auth.login"`)) ||
		!bytes.Contains(audit.Body.Bytes(), []byte(`"details":{}`)) {
		t.Fatalf("audit status=%d body=%s", audit.Code, audit.Body.String())
	}
}

func TestDashboardRequiresAdministrator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newTestApplication(t, "dashboard-rbac")
	admin := loginTestUser(t, app, "test@example.com", "test-password-123")
	created := requestWithToken(app.Router(), http.MethodPost, "/api/v1/admin/users",
		[]byte(`{"email":"viewer@example.com","display_name":"Viewer","password":"viewer-password-123","role":"user"}`), admin.AccessToken)
	if created.Code != http.StatusCreated {
		t.Fatalf("create user status=%d body=%s", created.Code, created.Body.String())
	}
	viewerUser, ok, err := app.store.GetUserByEmail("viewer@example.com")
	if err != nil || !ok {
		t.Fatalf("get viewer ok=%v err=%v", ok, err)
	}
	if _, err := app.store.UpdateUser(viewerUser.ID, viewerUser.DisplayName, viewerUser.Role, viewerUser.Status, false); err != nil {
		t.Fatal(err)
	}
	viewer := loginTestUser(t, app, "viewer@example.com", "viewer-password-123")
	response := requestWithToken(app.Router(), http.MethodGet, "/api/v1/admin/dashboard", nil, viewer.AccessToken)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-admin dashboard status=%d body=%s", response.Code, response.Body.String())
	}
}
