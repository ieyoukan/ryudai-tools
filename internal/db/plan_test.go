package db

import (
	"path/filepath"
	"testing"
)

func TestPlanItems(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	for _, it := range []PlanItem{
		{YearTerm: "2027後期", Title: "試験機械学習", Section: "専門教育", Category: "知能情報アドバンスト", Credits: 2},
		{YearTerm: "2027前期", Title: "卒業研究Ⅰ", Section: "専門教育", Category: "研究・基礎演習・実験", Credits: 3},
	} {
		if err := database.AddPlanItem("E000001", it); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.AddPlanItem("other", PlanItem{YearTerm: "2027前期", Title: "他人の科目", Credits: 2}); err != nil {
		t.Fatal(err)
	}
	items, err := database.PlanItems("e000001")
	if err != nil || len(items) != 2 || items[0].YearTerm != "2027前期" || items[1].Title != "試験機械学習" {
		t.Fatalf("items = %+v, err %v", items, err)
	}

	// 他人の科目は消せない
	others, _ := database.PlanItems("other")
	if err := database.DeletePlanItem("e000001", others[0].ID); err != nil {
		t.Fatal(err)
	}
	if others, _ = database.PlanItems("other"); len(others) != 1 {
		t.Fatal("deleted another user's plan item")
	}
	if err := database.DeletePlanItem("e000001", items[0].ID); err != nil {
		t.Fatal(err)
	}
	if items, _ = database.PlanItems("e000001"); len(items) != 1 {
		t.Fatalf("after delete = %+v", items)
	}
	if err := database.DeletePlan("e000001"); err != nil {
		t.Fatal(err)
	}
	if items, _ = database.PlanItems("e000001"); len(items) != 0 {
		t.Fatalf("after delete all = %+v", items)
	}
}

func TestLectureCategories(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(database.SaveLectureCategory("E000001", LectureCategory{LectureCode: "990327002", Category: "専門教育科目（知能情報）", Source: "kyomu"}))
	must(database.SaveLectureCategory("e000001", LectureCategory{LectureCode: "990342002", Category: "知能情報関連", Source: "user"}))
	// 利用者が選んだ区分は、教務システムから読んだ区分で上書きしない
	must(database.SaveLectureCategory("e000001", LectureCategory{LectureCode: "990342002", Category: "専門教育科目（研究・実験）", Source: "kyomu"}))
	// 教務システムの区分は利用者が選び直せる
	must(database.SaveLectureCategory("e000001", LectureCategory{LectureCode: "990327002", Category: "知能情報アドバンスト", Source: "user"}))

	got, err := database.LectureCategories("e000001")
	must(err)
	if got["990342002"].Category != "知能情報関連" || got["990327002"].Category != "知能情報アドバンスト" || got["990327002"].Source != "user" {
		t.Fatalf("categories = %+v", got)
	}
	if other, _ := database.LectureCategories("other"); len(other) != 0 {
		t.Fatalf("other user's categories = %+v", other)
	}
	must(database.DeleteLectureCategories("e000001"))
	if got, _ = database.LectureCategories("e000001"); len(got) != 0 {
		t.Fatalf("after delete = %+v", got)
	}
}
