package university

import "testing"

func TestRequiredSubjectsAllEngineeringCourses(t *testing.T) {
	for _, tc := range []struct {
		file                string
		required, selective int
	}{
		{"kikai", 33, 0}, {"energy", 28, 8}, {"denki", 25, 8}, {"denshi-", 25, 8},
		{"shakai", 28, 2}, {"kenchiku", 27, 0}, {"chinot", 31, 0},
	} {
		t.Run(tc.file, func(t *testing.T) {
			src := Source{URL: "https://official.example/handbook.pdf"}
			rows, err := ParseRequiredSubjects(fixture(t, "required2024_"+tc.file+".pdf"), Document{Title: tc.file}, src)
			if err != nil {
				t.Fatal(err)
			}
			required, selective := 0, 0
			for _, r := range rows {
				if tc.file == "shakai" && r.NumberingCode == "社基350" && (r.Title != "試験構造演習" || r.Credits != 2) {
					t.Fatal(r)
				}
				if tc.file == "kenchiku" && r.NumberingCode == "建築492" && (r.Title != "試験建築倫理" || r.Credits != 1) {
					t.Fatal(r)
				}
				if tc.file == "chinot" && r.NumberingCode == "知能301" && r.Title != "試験総合実験" {
					t.Fatal("next row leaked into course name", r)
				}
				if r.Selection == "required" {
					required++
				} else if r.Selection == "selective_required" {
					selective++
				} else {
					t.Fatal(r)
				}
				if r.Page != 1 || r.Source.URL != src.URL || r.Title == "" || r.Credits <= 0 {
					t.Fatal(r)
				}
				if r.Credits > 6 {
					t.Fatal("adjacent credits concatenated", r)
				}
				if tc.file == "chinot" && (r.NumberingCode == "知能210" && r.Title != "試験データベース" || r.NumberingCode == "知能221") {
					t.Fatal("row/mark mismatch", r)
				}
				if tc.file == "kenchiku" && r.NumberingCode == "工共100" {
					t.Fatal("another course's requirement applied")
				}
			}
			if required != tc.required || selective != tc.selective {
				t.Fatalf("required %d, selective %d; want %d, %d", required, selective, tc.required, tc.selective)
			}
		})
	}
}

func TestEnergyCourseRenameMatchesHandbook(t *testing.T) {
	if !CourseMatches("エネルギー環境工学コース", "エネルギー機械工学コース") {
		t.Fatal("renamed course not matched")
	}
	if CourseMatches("機械工学コース", "エネルギー機械工学コース") {
		t.Fatal("different courses matched")
	}
}
