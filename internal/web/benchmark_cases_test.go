package web

import "testing"

func TestGradeShiftAPKExpectedOutput(t *testing.T) {
	files := map[string]string{"schedule.json": `{"Mon":"Dan","Tue":"Ben","Wed":"Cara","Thu":"Ann"}`}
	passed, total, failures := gradeShift(files)
	if passed != total || total != 10 || len(failures) != 0 {
		t.Fatalf("passed=%d total=%d failures=%v", passed, total, failures)
	}
}

func TestGradeSalesAPKExpectedOutput(t *testing.T) {
	files := map[string]string{"report.json": `{
		"revenueByRegion":{"north":80,"south":80,"east":70},
		"topMonth":"2026-02","totalRevenue":230,"topRegion":"south"
	}`}
	passed, total, failures := gradeSales(files)
	if passed != total || total != 6 || len(failures) != 0 {
		t.Fatalf("passed=%d total=%d failures=%v", passed, total, failures)
	}
}

func TestGradeSalesAllowsSnakeCaseAndTieButRejectsWrongRegion(t *testing.T) {
	files := map[string]string{"report.json": `{
		"revenue_by_region":{"north":80,"south":80,"east":70},
		"top_month":"2026-02","total_revenue":230,"top_region":"west"
	}`}
	passed, total, failures := gradeSales(files)
	if passed != 5 || total != 6 || len(failures) != 1 {
		t.Fatalf("passed=%d total=%d failures=%v", passed, total, failures)
	}
}

func TestGradeLedgerAPKExpectedOutput(t *testing.T) {
	files := map[string]string{"state.json": `{
		"balances":{"A":0,"B":115,"C":30},"rejected":4,"applied":6
	}`}
	passed, total, failures := gradeLedger(files)
	if passed != total || total != 6 || len(failures) != 0 {
		t.Fatalf("passed=%d total=%d failures=%v", passed, total, failures)
	}
}

func TestGradeRouteAPKExpectedOutput(t *testing.T) {
	files := map[string]string{"route.json": `{"path":["A","C","B","D","E","F"],"cost":13}`}
	passed, total, failures := gradeRoute(files)
	if passed != total || total != 3 || len(failures) != 0 {
		t.Fatalf("passed=%d total=%d failures=%v", passed, total, failures)
	}
}

func TestGradeRouteRejectsMalformedArtifact(t *testing.T) {
	passed, total, failures := gradeRoute(map[string]string{"route.json": `not-json`})
	if passed != 0 || total != 3 || len(failures) != 1 {
		t.Fatalf("passed=%d total=%d failures=%v", passed, total, failures)
	}
}
