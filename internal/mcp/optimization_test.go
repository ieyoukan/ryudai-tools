package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/ieyoukan/daigaku-hack/internal/academic"
)

func TestLightweightSearchAndExplicitDetails(t *testing.T) {
	search, err := os.ReadFile("../provider/kyomu/testdata/syllabus_search.html")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := os.ReadFile("../provider/kyomu/testdata/syllabus_detail.html")
	if err != nil {
		t.Fatal(err)
	}
	var details, credentials atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "DetailMain.aspx") {
			details.Add(1)
			w.Write(detail)
		} else {
			w.Write(search)
		}
	}))
	defer upstream.Close()
	svc := academic.New(upstream.URL, func(string) (string, bool) { credentials.Add(1); return "", false })
	s := NewAcademicServer(svc, "test", nil, nil)
	invoke := func(args string) map[string]any {
		t.Helper()
		out := s.callTool(context.Background(), s.index["search_syllabus"], "student", "", json.RawMessage(args))
		if out["isError"] == true {
			t.Fatal(out)
		}
		return out
	}
	light := invoke(`{"subject":"プログラミング","year":2026}`)
	var res map[string]any
	if err := json.Unmarshal(light["structuredContent"].(json.RawMessage), &res); err != nil {
		t.Fatal(err)
	}
	if res["returned"] != float64(10) || res["truncated"] != true || res["total"] != float64(30) {
		t.Fatal(res)
	}
	first := res["results"].([]any)[0].(map[string]any)
	if _, ok := first["credits"]; ok {
		t.Fatal("unfetched credits must not be reported as zero")
	}
	if first["lecture_code"] == nil {
		t.Fatal("summary fields must remain flat")
	}
	if details.Load() != 0 || credentials.Load() != 0 {
		t.Fatal("light search fetched details or private data")
	}
	full := invoke(`{"subject":"プログラミング","year":2026,"limit":20,"include_details":true}`)
	if details.Load() != 20 || credentials.Load() != 0 {
		t.Fatalf("detail requests=%d private=%d", details.Load(), credentials.Load())
	}
	lightBytes, _ := json.Marshal(light)
	fullBytes, _ := json.Marshal(full)
	if len(lightBytes) >= len(fullBytes) {
		t.Fatal("light response was not smaller")
	}
	t.Logf("fixture: detail requests 20 → 0, MCP response %d → %d bytes", len(fullBytes), len(lightBytes))
	// Explicit private enrichment still works and reports missing credentials as unknown, not zero seats.
	enriched := invoke(`{"subject":"プログラミング","year":2026,"limit":1,"include_availability":true}`)
	var private map[string]any
	json.Unmarshal(enriched["structuredContent"].(json.RawMessage), &private)
	availability := private["results"].([]any)[0].(map[string]any)["availability"].(map[string]any)
	if availability["status"] != "unavailable" {
		t.Fatal(availability)
	}
	if _, ok := availability["seats_left"]; ok {
		t.Fatal("unknown seats must not become zero")
	}
	for _, name := range []string{"get_syllabus", "describe_lectures"} {
		for _, live := range []bool{false, true} {
			args, _ := json.Marshal(map[string]any{"year": 2026, "lecture_code": "990000001", "lecture_codes": []string{"990000001"}, "include_availability": live})
			result := s.callTool(context.Background(), s.index[name], "student", "", args)
			if result["isError"] == true {
				t.Fatal(result)
			}
			var payload map[string]any
			if err := json.Unmarshal(result["structuredContent"].(json.RawMessage), &payload); err != nil {
				t.Fatal(err)
			}
			if name == "describe_lectures" {
				payload = payload["lectures"].([]any)[0].(map[string]any)
			}
			status := payload["availability"].(map[string]any)["status"]
			want := "public_only"
			if live {
				want = "unavailable"
			}
			if status != want {
				t.Errorf("%s live=%v status=%v", name, live, status)
			}
		}
	}

}

func TestToolDescriptionBudget(t *testing.T) {
	s := NewAcademicServer(academic.New("", nil), "test", nil, nil)
	for _, tool := range s.tools {
		if n := utf8.RuneCountInString(tool.Description); n > 150 {
			t.Errorf("%s description too long: %d", tool.Name, n)
		}
	}
}
