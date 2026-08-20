package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminModelsEndpointMatchesOriginalAPKCatalog(t *testing.T) {
	s := &Server{settings: &settingsStore{v: originalCatalogSettings()}}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/models", nil)
	w := httptest.NewRecorder()
	s.adminModels(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	var body struct {
		Object string           `json:"object"`
		Data   []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Object != "list" || len(body.Data) != 14 {
		t.Fatalf("body=%#v", body)
	}
	want := []string{"gpt-5.2", "gpt-5.3-reasoning", "claude-sonnet", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"}
	seen := map[string]bool{}
	for _, model := range body.Data {
		id, _ := model["id"].(string)
		if strings.HasSuffix(strings.ToLower(id), "-chat") {
			t.Fatalf("internal tone leaked as public model ID: %#v", model)
		}
		if _, exists := model["configured"]; exists {
			t.Fatalf("recovered-only configured flag leaked into API: %#v", model)
		}
		seen[id] = true
	}
	for _, id := range want {
		if !seen[id] {
			t.Fatalf("missing original model %q from %#v", id, body.Data)
		}
	}
}
