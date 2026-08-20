package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

// The fixtures were captured by executing the original ARM64 libm365.so under
// QEMU on 2026-08-20. Comparing JSON values rather than bytes deliberately
// ignores map-key serialization order while preserving the complete endpoint
// contract, including null versus omitted fields and numeric values.
func assertJSONMatchesAPKFixture(t *testing.T, fixture string, got []byte) {
	t.Helper()
	want, err := os.ReadFile("testdata/" + fixture)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixture, err)
	}
	var wantValue, gotValue any
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("decode fixture %s: %v", fixture, err)
	}
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode response for %s: %v", fixture, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("endpoint response differs from original APK fixture %s", fixture)
	}
}

func TestAdminModelsEndpointMatchesOriginalAPKFixture(t *testing.T) {
	s := &Server{settings: &settingsStore{v: originalCatalogSettings()}}
	w := httptest.NewRecorder()
	s.adminModels(w, httptest.NewRequest(http.MethodGet, "/api/admin/models", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	assertJSONMatchesAPKFixture(t, "original-admin-models.json", w.Body.Bytes())
}

func TestAdminSettingsEndpointMatchesOriginalAPKFixture(t *testing.T) {
	settings := originalCatalogSettings()
	settings.MaxToolCallsPerTurn = 32
	settings.MaxToolRounds = 512
	settings.LogLevel = "info"
	settings.ListenAddress = "127.0.0.1:4152"
	settings.ToolPlanningMode = "router"
	settings.ClientProfile = "office"
	s := &Server{settings: &settingsStore{v: settings}}
	w := httptest.NewRecorder()
	s.adminSettings(w, httptest.NewRequest(http.MethodGet, "/api/admin/settings", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	assertJSONMatchesAPKFixture(t, "original-admin-settings.json", w.Body.Bytes())
}
