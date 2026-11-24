package university

import (
	"os"
	"testing"
	"time"
)

func TestParsedTimetableCachePreservesFreshSourceAndIsolatesReturnedRows(t *testing.T) {
	c := New()
	b := fixture(t, "fusion2026.pdf")
	doc := Document{Title: "工学融合"}
	first := Source{URL: "official.pdf", FetchedAt: time.Now()}
	rows, err := c.timetableRows(b, doc, first)
	if err != nil || len(rows) == 0 {
		t.Fatalf("%v", err)
	}
	credits, capacity := *rows[0].Credits, *rows[0].Capacity
	*rows[0].Credits = 99
	*rows[0].Capacity = 99
	rows[0].Title = "mutated"
	fresh := Source{URL: first.URL, FetchedAt: first.FetchedAt.Add(time.Hour), Stale: true, Warning: "refresh failed"}
	again, err := c.timetableRows(b, doc, fresh)
	if err != nil || len(c.parsed) != 1 || again[0].Title == "mutated" || *again[0].Credits != credits || *again[0].Capacity != capacity || again[0].Source != fresh {
		t.Fatalf("cached data or source corrupted: %+v %v", again, err)
	}
	_, err = c.timetableRows(fixture(t, "course2026.pdf"), doc, fresh)
	if err != nil || len(c.parsed) != 2 {
		t.Fatal("changed content did not invalidate cache", err)
	}
}

func TestParsedPageCacheAlsoCachesUnreadableDocuments(t *testing.T) {
	c := New()
	for i := 0; i < 3; i++ {
		if _, err := c.documentPages([]byte("not PDF"), Document{Title: "訂正表"}); err == nil {
			t.Fatal("bad PDF accepted")
		}
	}
	if len(c.parsed) != 1 {
		t.Fatal("same failed parse repeated")
	}
}

func BenchmarkCachedTimetableRows(b *testing.B) {
	data, err := os.ReadFile("testdata/fusion2026.pdf")
	if err != nil {
		b.Fatal(err)
	}
	c := New()
	doc := Document{Title: "工学融合"}
	if _, err := c.timetableRows(data, doc, Source{}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := c.timetableRows(data, doc, Source{}); err != nil {
			b.Fatal(err)
		}
	}
}
