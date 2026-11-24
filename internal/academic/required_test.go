package academic

import (
	"testing"

	"github.com/ieyoukan/ryudai-tools/internal/provider/kyomu"
	"github.com/ieyoukan/ryudai-tools/internal/provider/university"
)

func TestRequiredSubjectUsesNumberAndCreditsAndPassedTakesPriority(t *testing.T) {
	subject := university.RequiredSubject{NumberingCode: "知能206", Title: "データサイエンス基礎", Credits: 4, Selection: "required", Page: 2, Source: university.Source{URL: "official.pdf"}}
	ids := map[string]syllabusIdentity{"2024/old": {Number: "知能206"}, "2025/retake": {Number: "知能206"}, "2026/new": {Number: "工共122"}}
	grades := &kyomu.GradeList{Grades: []kyomu.Grade{{Year: 2024, LectureCode: "old", Title: subject.Title, Credits: 4, Passed: true, Grade: "R"}, {Year: 2025, LectureCode: "retake", Title: subject.Title, Credits: 4, Passed: false, Grade: "F"}, {Year: 2026, LectureCode: "new", Title: subject.Title, Credits: 2, Passed: true, Grade: "A"}}}
	r := checkRequiredSubject(subject, grades, nil, nil, ids)
	if r.Status != "passed" || r.Page != 2 || r.Source.URL != "official.pdf" {
		t.Fatal(r)
	}
	grades.Grades = grades.Grades[2:]
	r = checkRequiredSubject(subject, grades, nil, nil, ids)
	if r.Status != "unverified" {
		t.Fatal("same title with different number counted as passed", r)
	}
	grades.Grades = []kyomu.Grade{{Year: 2024, LectureCode: "old", Title: subject.Title, Credits: 2, Passed: true}, {Year: 2025, LectureCode: "retake", Title: subject.Title, Credits: 2, Passed: true}}
	r = checkRequiredSubject(subject, grades, nil, nil, ids)
	if r.Status != "unverified" {
		t.Fatal("retake credits combined", r)
	}
}

func TestRequiredSubjectUnknownEnrolledAndPlannedAreDistinct(t *testing.T) {
	subject := university.RequiredSubject{NumberingCode: "工共401", Title: "卒業研究Ⅰ", Credits: 3}
	ids := map[string]syllabusIdentity{"2026/a": {Number: "工共401"}}
	if r := checkRequiredSubject(subject, nil, nil, nil, ids); r.Status != "unverified" {
		t.Fatal(r)
	}
	grades := &kyomu.GradeList{Grades: []kyomu.Grade{}}
	if r := checkRequiredSubject(subject, grades, nil, nil, ids); r.Status != "unverified" {
		t.Fatal("missing registration became definitive not_passed", r)
	}
	reg := &kyomu.Registration{YearTerm: "2026年度後期", Lectures: []kyomu.RegisteredLecture{}}
	if r := checkRequiredSubject(subject, grades, reg, nil, ids); r.Status != "not_passed" {
		t.Fatal(r)
	}
	reg.Lectures = []kyomu.RegisteredLecture{{Rank: 2, LectureCode: "a", Title: "卒業研究I", Credits: 3}}
	if r := checkRequiredSubject(subject, grades, reg, nil, ids); r.Status != "not_passed" {
		t.Fatal("second wish counted", r)
	}
	reg.Lectures[0].Rank = 1
	if r := checkRequiredSubject(subject, grades, reg, nil, ids); r.Status != "enrolled" {
		t.Fatal(r)
	}
	plan := []PlanItem{{LectureCode: "a", Title: "卒業研究I", NumberingCode: "工共401", Credits: 3}}
	if r := checkRequiredSubject(subject, grades, nil, plan, ids); r.Status != "planned" {
		t.Fatal(r)
	}
	if subjectNameKey("卒業研究 Ⅰ") != subjectNameKey("卒業研究Ｉ") {
		t.Fatal("notation variants differ")
	}
	if subjectNameKey("工業数学Ⅰ") == subjectNameKey("工業数学Ⅱ") {
		t.Fatal("different courses collapsed")
	}
}

func TestRequiredSubjectAcceptsRenamedSubjectOnlyWithSameNumber(t *testing.T) {
	subject := university.RequiredSubject{NumberingCode: "知能212", Title: "ソフトウエア開発演習I", Credits: 2}
	grades := &kyomu.GradeList{Grades: []kyomu.Grade{{Year: 2024, LectureCode: "a", Title: "ソフトウェア開発演習Ⅰ", Credits: 2, Passed: true}}}
	if r := checkRequiredSubject(subject, grades, nil, nil, map[string]syllabusIdentity{"2024/a": {Number: "知能212"}}); r.Status != "passed" {
		t.Fatal(r)
	}
}
