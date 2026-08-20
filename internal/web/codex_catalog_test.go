package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func originalCatalogSettings() runtimeSettings {
	return runtimeSettings{
		ContextWindow:       262144,
		MaxOutputTokens:     16384,
		ChatTimeoutSeconds:  600,
		ImageTimeoutSeconds: 180,
		ModelMappings:       append([]modelMapping(nil), defaultModelMappings...),
	}
}

func TestModelTokenLimitsAreConsistent(t *testing.T) {
	l := modelLimitsForSettings(runtimeSettings{ContextWindow: 128000, MaxOutputTokens: 16384})
	if l.ContextWindow != 128000 || l.MaxOutputTokens != 16384 || l.MaxInputTokens != 111616 {
		t.Fatalf("limits=%+v", l)
	}
}

func TestModelTokenLimitsNormalizeBadOutputLimit(t *testing.T) {
	l := modelLimitsForSettings(runtimeSettings{ContextWindow: 100, MaxOutputTokens: 500})
	if l.MaxInputTokens <= 0 || l.MaxOutputTokens <= 0 || l.MaxInputTokens+l.MaxOutputTokens != l.ContextWindow {
		t.Fatalf("inconsistent limits=%+v", l)
	}
}

func TestKnownUpstreamTonesMatchOriginalAPK(t *testing.T) {
	want := []string{
		"Gpt_5_2_Chat", "Gpt_5_2_Reasoning", "Gpt_5_3_Chat", "Gpt_5_3_Reasoning",
		"Gpt_5_4_Chat", "Gpt_5_4_Reasoning", "Gpt_5_5_Chat", "Gpt_5_5_Reasoning",
		"Gpt_5_6_Reasoning", "Claude_Sonnet", "Claude_Sonnet_Reasoning",
	}
	got := knownUpstreamTones()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tones=%q, want %q", got, want)
	}
}

func TestModelCatalogMatchesOriginalAPK(t *testing.T) {
	type expectedModel struct {
		id, owner, displayName string
	}
	want := []expectedModel{
		{"gpt-5.2", "microsoft-365", "gpt-5.2"},
		{"gpt-5.2-reasoning", "microsoft-365", "gpt-5.2-reasoning"},
		{"gpt-5.3", "microsoft-365", "gpt-5.3"},
		{"gpt-5.3-reasoning", "microsoft-365", "gpt-5.3-reasoning"},
		{"gpt-5.4", "microsoft-365", "gpt-5.4"},
		{"gpt-5.4-reasoning", "microsoft-365", "gpt-5.4-reasoning"},
		{"gpt-5.5", "microsoft-365", "gpt-5.5"},
		{"gpt-5.5-reasoning", "microsoft-365", "gpt-5.5-reasoning"},
		{"gpt-5.6-reasoning", "microsoft-365", "gpt-5.6-reasoning"},
		{"claude-sonnet", "anthropic-via-microsoft-365", "claude-sonnet"},
		{"claude-sonnet-reasoning", "anthropic-via-microsoft-365", "claude-sonnet-reasoning"},
		{"gpt-5.6-sol", "microsoft-365", "GPT-5.6-Sol"},
		{"gpt-5.6-terra", "microsoft-365", "GPT-5.6-Terra"},
		{"gpt-5.6-luna", "microsoft-365", "GPT-5.6-Luna"},
	}
	models := modelCatalogForSettings(originalCatalogSettings())
	if len(models) != len(want) {
		t.Fatalf("catalog count=%d, want %d", len(models), len(want))
	}
	for i, expected := range want {
		model := models[i]
		if model["id"] != expected.id || model["owned_by"] != expected.owner || model["display_name"] != expected.displayName {
			t.Fatalf("model[%d]=%#v, want id=%q owner=%q display=%q", i, model, expected.id, expected.owner, expected.displayName)
		}
		if _, ok := model["configured"]; ok {
			t.Fatalf("model[%d] leaked recovered-only configured flag: %#v", i, model)
		}
		if strings.HasSuffix(strings.ToLower(expected.id), "-chat") {
			t.Fatalf("public model ID must not expose an upstream chat suffix: %q", expected.id)
		}
		if model["default_reasoning_level"] != "xhigh" || model["context_window"] != 262144 || model["max_input_tokens"] != 245760 || model["max_output_tokens"] != 16384 {
			t.Fatalf("model[%d] metadata=%#v", i, model)
		}
	}
}

func TestStaticCatalogSurvivesAbsentMappings(t *testing.T) {
	settings := originalCatalogSettings()
	settings.ModelMappings = nil
	models := modelCatalogForSettings(settings)
	want := []string{
		"gpt-5.2", "gpt-5.2-reasoning", "gpt-5.3", "gpt-5.3-reasoning",
		"gpt-5.4", "gpt-5.4-reasoning", "gpt-5.5", "gpt-5.5-reasoning",
		"gpt-5.6-reasoning", "claude-sonnet", "claude-sonnet-reasoning",
	}
	if len(models) != len(want) {
		t.Fatalf("static catalog count=%d, want %d", len(models), len(want))
	}
	for i, id := range want {
		if models[i]["id"] != id {
			t.Fatalf("static model[%d]=%#v, want %q", i, models[i], id)
		}
	}
}

func TestConfiguredAliasesReplaceOnlyAliasSlotsAsOriginalAPK(t *testing.T) {
	settings := originalCatalogSettings()
	settings.ModelMappings = []modelMapping{
		{PublicModel: "custom-alpha", UpstreamTone: "Gpt_5_6_Reasoning", DisplayName: "Custom Alpha", DefaultReasoningLevel: "xhigh"},
		{PublicModel: "custom-beta", UpstreamTone: "Gpt_5_6_Reasoning", DisplayName: "Custom Beta", DefaultReasoningLevel: "xhigh"},
		{PublicModel: "custom-gamma", UpstreamTone: "Gpt_5_6_Reasoning", DisplayName: "Custom Gamma", DefaultReasoningLevel: "xhigh"},
	}
	models := modelCatalogForSettings(settings)
	want := []string{
		"gpt-5.2", "gpt-5.2-reasoning", "gpt-5.3", "gpt-5.3-reasoning",
		"gpt-5.4", "gpt-5.4-reasoning", "gpt-5.5", "gpt-5.5-reasoning",
		"gpt-5.6-reasoning", "claude-sonnet", "claude-sonnet-reasoning",
		"custom-alpha", "custom-beta", "custom-gamma",
	}
	if len(models) != len(want) {
		t.Fatalf("custom catalog count=%d, want %d", len(models), len(want))
	}
	for i, id := range want {
		if models[i]["id"] != id {
			t.Fatalf("custom model[%d]=%#v, want %q", i, models[i], id)
		}
	}
	if models[11]["display_name"] != "Custom Alpha" || models[12]["display_name"] != "Custom Beta" || models[13]["display_name"] != "Custom Gamma" {
		t.Fatalf("custom alias metadata=%#v", models[11:])
	}
}

func TestInternalToneRoutingDoesNotChangePublicModelNames(t *testing.T) {
	mappings := append([]modelMapping(nil), defaultModelMappings...)
	cases := []struct {
		model, effort, want string
	}{
		{"gpt-5.2", "", "Gpt_5_2_Chat"},
		{"gpt-5.2-reasoning", "", "Gpt_5_2_Reasoning"},
		{"gpt-5.3", "", "Gpt_5_3_Chat"},
		{"gpt-5.3-reasoning", "", "Gpt_5_3_Reasoning"},
		{"gpt-5.4", "", "Gpt_5_4_Chat"},
		{"gpt-5.4-reasoning", "", "Gpt_5_4_Reasoning"},
		{"gpt-5.5", "", "Gpt_5_5_Chat"},
		{"gpt-5.5-reasoning", "", "Gpt_5_5_Reasoning"},
		{"gpt-5.6-reasoning", "", "Gpt_5_6_Reasoning"},
		{"claude-sonnet", "", "Claude_Sonnet"},
		{"claude-sonnet-reasoning", "", "Claude_Sonnet_Reasoning"},
		{"gpt-5.6-sol", "", "Gpt_5_6_Reasoning"},
		{"gpt-5.6-terra", "", "Gpt_5_6_Reasoning"},
		{"gpt-5.6-luna", "", "Gpt_5_6_Reasoning"},
		{"gpt-5.2", "high", "Gpt_5_2_Reasoning"},
		{"claude-sonnet", "high", "Claude_Sonnet_Reasoning"},
		{"auto", "", "magic"},
	}
	for _, tc := range cases {
		got, err := reasoningToneForMappings(tc.model, tc.effort, mappings)
		if err != nil || got != tc.want {
			t.Fatalf("route %s/%s = %q, %v; want %q", tc.model, tc.effort, got, err, tc.want)
		}
	}
}

func TestModelsAdvertiseContextAndReasoning(t *testing.T) {
	s := &Server{settings: &settingsStore{v: originalCatalogSettings()}}
	r := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	s.openaiModels(w, r)
	var body struct {
		Data   []map[string]any `json:"data"`
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) == 0 {
		t.Fatal("empty model catalog")
	}
	if len(body.Models) != len(body.Data) {
		t.Fatalf("models alias length=%d, data length=%d", len(body.Models), len(body.Data))
	}
	if len(body.Data) != 14 {
		t.Fatalf("model catalog count=%d, want 14", len(body.Data))
	}
	for _, m := range body.Data {
		if m["description"] != "Microsoft 365 gateway model route." {
			t.Fatalf("model catalog exposes provider details: %#v", m)
		}
		baseInstructions, ok := m["base_instructions"].(string)
		if !ok || baseInstructions == "" {
			t.Fatalf("missing Codex base instructions: %#v", m)
		}
		modelMessages, ok := m["model_messages"].(map[string]any)
		if !ok || modelMessages["instructions_template"] != baseInstructions {
			t.Fatalf("missing or inconsistent Codex model messages: %#v", m)
		}
		variables, ok := modelMessages["instructions_variables"].(map[string]any)
		if !ok || variables["personality_default"] != "" || variables["personality_friendly"] != "" || variables["personality_pragmatic"] != "" {
			t.Fatalf("invalid Codex instruction variables: %#v", modelMessages)
		}
		if modelMessages["approvals"] != nil || modelMessages["auto_review"] != nil {
			t.Fatalf("invalid optional Codex model messages: %#v", modelMessages)
		}
		if m["slug"] != m["id"] {
			t.Fatalf("missing or inconsistent slug: %#v", m)
		}
		if displayName, ok := m["display_name"].(string); !ok || displayName == "" {
			t.Fatalf("missing display_name: %#v", m)
		}
		levels, ok := m["supported_reasoning_levels"].([]any)
		if !ok || len(levels) == 0 {
			t.Fatalf("missing supported reasoning levels: %#v", m)
		}
		for _, level := range levels {
			preset, ok := level.(map[string]any)
			if !ok || preset["effort"] == "" || preset["description"] == "" {
				t.Fatalf("invalid reasoning preset: %#v", level)
			}
		}
		defaultReasoningLevel, ok := m["default_reasoning_level"].(string)
		if effort, err := normalizeReasoningEffort(defaultReasoningLevel); !ok || err != nil || effort == "" || m["description"] == "" {
			t.Fatalf("missing Codex catalog metadata: %#v", m)
		}
		if m["shell_type"] != "shell_command" || m["visibility"] != "list" || m["supported_in_api"] != true || m["priority"] != float64(1) {
			t.Fatalf("missing Codex execution metadata: %#v", m)
		}
		if _, ok := m["additional_speed_tiers"].([]any); !ok {
			t.Fatalf("missing speed tiers: %#v", m)
		}
		if _, ok := m["service_tiers"].([]any); !ok {
			t.Fatalf("missing service tiers: %#v", m)
		}
		if m["apply_patch_tool_type"] != "freeform" || m["web_search_tool_type"] != "text_and_image" || m["tool_mode"] != "code_mode_only" || m["multi_agent_version"] != "v2" {
			t.Fatalf("missing Codex tool metadata: %#v", m)
		}
		if m["max_context_window"] != m["context_window"] || m["effective_context_window_percent"] != float64(95) {
			t.Fatalf("inconsistent Codex context metadata: %#v", m)
		}
		policy, ok := m["truncation_policy"].(map[string]any)
		if !ok || policy["mode"] != "tokens" || policy["limit"] != float64(10000) {
			t.Fatalf("missing truncation policy: %#v", m)
		}
		if _, ok := m["experimental_supported_tools"].([]any); !ok || m["supports_search_tool"] != true || m["use_responses_lite"] != false {
			t.Fatalf("missing Codex capability metadata: %#v", m)
		}
		if m["context_window"].(float64) <= 0 || m["max_input_tokens"].(float64) <= 0 || m["max_output_tokens"].(float64) <= 0 {
			t.Fatalf("missing limits: %#v", m)
		}
		caps, ok := m["capabilities"].(map[string]any)
		if !ok {
			t.Fatalf("missing capabilities: %#v", m)
		}
		if caps["reasoning"] != true {
			t.Fatalf("reasoning not advertised: %#v", m)
		}
		if levels, ok := caps["supported_reasoning_levels"].([]any); !ok || len(levels) == 0 {
			t.Fatalf("capabilities missing supported reasoning levels: %#v", m)
		}
		if efforts, ok := caps["reasoning_efforts"].([]any); !ok || len(efforts) == 0 {
			t.Fatalf("capabilities missing object reasoning efforts: %#v", m)
		} else if _, ok := efforts[0].(map[string]any); !ok {
			t.Fatalf("reasoning efforts must be preset objects: %#v", efforts)
		}
	}
	for i, m := range body.Models {
		if m["slug"] != body.Data[i]["slug"] {
			t.Fatalf("models alias missing slug at %d: %#v", i, m)
		}
		if m["display_name"] != body.Data[i]["display_name"] {
			t.Fatalf("models alias missing display_name at %d: %#v", i, m)
		}
		if m["supported_reasoning_levels"] == nil {
			t.Fatalf("models alias missing supported reasoning levels at %d: %#v", i, m)
		}
		if m["base_instructions"] != body.Data[i]["base_instructions"] || m["model_messages"] == nil {
			t.Fatalf("models alias missing instruction metadata at %d: %#v", i, m)
		}
	}
}

func TestConfiguredModelMappingsDriveCatalogAndRouting(t *testing.T) {
	mappings := []modelMapping{{PublicModel: "gpt-5.6-sol", UpstreamTone: "Gpt_5_6_Reasoning", DisplayName: "GPT-5.6-Sol", DefaultReasoningLevel: "low"}}
	models := configuredModelSpecs(mappings)
	if len(models) != len(gatewayModels)+1 || models[len(models)-1].ID != "gpt-5.6-sol" || models[len(models)-1].DefaultReasoningLevel != "low" {
		t.Fatalf("configured models=%#v", models)
	}
	mapping, ok := configuredModelMapping("GPT-5.6-SOL", mappings)
	if !ok || mapping.UpstreamTone != "Gpt_5_6_Reasoning" {
		t.Fatalf("mapping=%#v ok=%t", mapping, ok)
	}
	if tone, ok := configuredModelTone("gpt-5.6-sol", mappings); !ok || tone != "Gpt_5_6_Reasoning" {
		t.Fatalf("tone=%q ok=%t", tone, ok)
	}
	override := configuredModelSpecs([]modelMapping{{PublicModel: "gpt-5.5", UpstreamTone: "Gpt_5_5_Reasoning", DisplayName: "GPT-5.5", DefaultReasoningLevel: "high"}})
	if len(override) != len(gatewayModels) || override[6].DefaultReasoningLevel != "high" {
		t.Fatalf("built-in override=%#v", override)
	}
}

func TestModelCatalogIncludesMaxReasoningPreset(t *testing.T) {
	for _, model := range modelCatalogForSettings(originalCatalogSettings()) {
		levels, ok := model["supported_reasoning_levels"].([]reasoningEffortPreset)
		if !ok {
			t.Fatalf("reasoning levels have unexpected type: %T", model["supported_reasoning_levels"])
		}
		for _, level := range levels {
			if level.Effort == "max" && level.Description != "" {
				return
			}
		}
	}
	t.Fatal("model catalog does not advertise a described max reasoning preset")
}

func TestReasoningEffortRouting(t *testing.T) {
	cases := []struct{ model, effort, want string }{
		{"claude-sonnet", "none", "Claude_Sonnet"},
		{"claude-sonnet", "high", "Claude_Sonnet_Reasoning"},
		{"gpt-5.5", "low", "Gpt_5_5_Chat"},
		{"gpt-5.5", "medium", "Gpt_5_5_Reasoning"},
		{"gpt-5.6-reasoning", "none", "Gpt_5_6_Reasoning"},
		{"gpt-5.6-reasoning", "max", "Gpt_5_6_Reasoning"},
	}
	mappings := append([]modelMapping(nil), defaultModelMappings...)
	for _, tc := range cases {
		got, err := reasoningToneForMappings(tc.model, tc.effort, mappings)
		if err != nil || got != tc.want {
			t.Fatalf("%s/%s got=%q err=%v", tc.model, tc.effort, got, err)
		}
	}
	if _, err := reasoningToneForMappings("gpt-5.6-reasoning", "extreme", defaultModelMappings); err == nil {
		t.Fatal("invalid effort accepted")
	}
}

func TestChatRejectsInvalidReasoningBeforeUpstream(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.6-reasoning","reasoning_effort":"extreme","messages":[{"role":"user","content":"hello"}]}`))
	w := httptest.NewRecorder()
	s.openaiChat(w, r)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "unsupported reasoning effort") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestResponsesReasoningConvertsToOpenAI(t *testing.T) {
	r := responsesRequest{Model: "gpt-5.6-reasoning", Input: "hello", Reasoning: &reasoningConfig{Effort: "high"}}
	o, err := r.openAI()
	if err != nil {
		t.Fatal(err)
	}
	if o.ReasoningEffort != "high" {
		t.Fatalf("effort=%q", o.ReasoningEffort)
	}
}
