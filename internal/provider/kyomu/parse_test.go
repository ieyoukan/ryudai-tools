package kyomu

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func loadFixture(t *testing.T, name, pageURL string) *page {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	p, err := parseHTML(pageURL, string(b))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseSearchResults(t *testing.T) {
	p := loadFixture(t, "syllabus_search.html", DefaultBaseURL+"/Public/Syllabus/")
	got := parseSearchResults(p, DefaultBaseURL, 2026)
	if len(got) != 30 {
		t.Fatalf("件数 = %d, want 30", len(got))
	}
	var prog *SyllabusSummary
	for i := range got {
		if got[i].LectureCode == "990000001" {
			prog = &got[i]
		}
	}
	if prog == nil {
		t.Fatal("990000001 が見つからない")
	}
	want := SyllabusSummary{
		Year: 2026, LectureCode: "990000001", NumberingCode: "試験001", Language: "日",
		Title: "試験プログラミング基礎", Instructors: "試験 教員", Term: "前学期", DayPeriod: "月3",
		Slots: []Slot{{Day: "月", Period: 3}},
		URL:   DefaultBaseURL + "/Public/Syllabus/DetailMain.aspx?lct_year=2026&lct_cd=990000001&je_cd=1",
	}
	if !reflect.DeepEqual(*prog, want) {
		t.Errorf("got  %+v\nwant %+v", *prog, want)
	}
	if !p.hasGridPager() {
		t.Error("ページャ（Page$2）を検出できていない")
	}
}

func TestParseSyllabus(t *testing.T) {
	p := loadFixture(t, "syllabus_detail.html", DefaultBaseURL+"/Public/Syllabus/DetailMain.aspx")
	s, err := parseSyllabus(p)
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string][2]any{
		"LectureCode":        {s.LectureCode, "990000005"},
		"Title":              {s.Title, "試験プログラミングⅠ 1組"},
		"Credits":            {s.Credits, 2.0},
		"Year":               {s.Year, 2026},
		"Term":               {s.Term, "後学期"},
		"DayPeriod":          {s.DayPeriod, "火5"},
		"Faculty":            {s.Faculty, "工学部工学科"},
		"Room":               {s.Room, "[火5]試験棟221"},
		"Quota":              {s.Quota, "20"},
		"RegistrationMethod": {s.RegistrationMethod, "抽選対象"},
		"TeachingFormat":     {s.TeachingFormat, "対面授業"},
		"MainCourse":         {s.MainCourse, true},
		"Language":           {s.Language, "日本語"},
	}
	for name, c := range checks {
		if !reflect.DeepEqual(c[0], c[1]) {
			t.Errorf("%s = %#v, want %#v", name, c[0], c[1])
		}
	}
	if !reflect.DeepEqual(s.LectureTypes, []string{"講義", "演習又は実験", "TA有り"}) {
		t.Errorf("LectureTypes = %v", s.LectureTypes)
	}
	if !strings.Contains(s.Plan, "第 1回 準備") || !strings.Contains(s.Plan, "\n第 2回") {
		t.Errorf("Plan の改行が保持されていない: %q", s.Plan)
	}
	if !strings.Contains(s.Grading, "60％以上") {
		t.Errorf("Grading = %q", s.Grading)
	}
	if len(s.References) != 1 || s.References[0].ISBN != "9780000000002" || s.References[0].Author != "試験 著者" {
		t.Errorf("References = %+v", s.References)
	}
	if len(s.Textbooks) != 0 {
		t.Errorf("Textbooks = %+v", s.Textbooks)
	}
}

func TestParseSyllabusNotFound(t *testing.T) {
	p, _ := parseHTML("x", "<html><body><form></form></body></html>")
	if _, err := parseSyllabus(p); err != ErrSyllabusNotFound {
		t.Errorf("err = %v, want ErrSyllabusNotFound", err)
	}
}

func TestParseRegistrationEdit(t *testing.T) {
	r := parseRegistration(loadFixture(t, "regist_edit.html", "https://example/RegistEdit.aspx"))
	if !r.Open {
		t.Error("登録期間中（追加ボタンあり）なのに Open=false")
	}
	if r.YearTerm != "2026年度後期" {
		t.Errorf("YearTerm = %q", r.YearTerm)
	}
	wantCredits := CreditSummary{Limit: 20, LimitedCredits: 6, ExemptCredits: 2, TotalCredits: 8, RemainingCredit: 14}
	if r.Credits == nil || *r.Credits != wantCredits {
		t.Errorf("Credits = %+v", r.Credits)
	}
	type key struct {
		Day    string
		Period int
		Rank   int
		Code   string
	}
	var got []key
	for _, l := range r.Lectures {
		got = append(got, key{l.Day, l.Period, l.Rank, l.LectureCode})
	}
	want := []key{
		{"水", 3, 1, "990000001"},
		{"水", 3, 2, "990000002"},
		{"木", 5, 1, "990000003"},
		{"集中等", 1, 1, "990000004"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
	first := r.Lectures[0]
	if first.Title != "試験プログラミング基礎" || first.Credits != 2 || first.Category != "専門教育科目（専門基礎）" || first.Year != 2026 {
		t.Errorf("lecture = %+v", first)
	}
}

func TestParseRegistrationList(t *testing.T) {
	r := parseRegistration(loadFixture(t, "regist_list.html", "https://example/RegistList.aspx"))
	if r.Open {
		t.Error("履修時間割（ボタン無し）なのに Open=true")
	}
	if len(r.Lectures) != 2 || r.Lectures[0].Day != "火" || r.Lectures[1].Day != "水" {
		t.Errorf("Lectures = %+v", r.Lectures)
	}
}

func TestParseCandidates(t *testing.T) {
	got := parseCandidates(loadFixture(t, "candidates.html", "https://example/RegistSearchResults.aspx"))
	if len(got) != 11 {
		t.Fatalf("件数 = %d, want 11", len(got))
	}
	c := got[0]
	want := Candidate{
		Faculty: "共通教育等科目", LectureCode: "990000101", Title: "試験心理学(振替) 04組",
		Instructors: "試験 教員", DayPeriod: "月2", Slots: []Slot{{Day: "月", Period: 2}},
		Credits: 2, Capacity: 120, CapacityKnown: true, Registered: 49, RegisteredKnown: true, Method: "Web教員許可制", Year: 2026, Addable: true,
	}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("got  %+v\nwant %+v", c, want)
	}
	var over bool
	for _, c := range got {
		if c.LectureCode == "990000102" { // 定員40に87人
			over = c.OverCapacity
		}
	}
	if !over {
		t.Error("定員超過を検出できていない")
	}
}

func TestParseSlots(t *testing.T) {
	cases := map[string][]Slot{
		"月3":      {{"月", 3, ""}},
		"月2,木3":   {{"月", 2, ""}, {"木", 3, ""}},
		"月3〜4前半":  {{"月", 3, "前半"}, {"月", 4, "前半"}},
		"集中":      nil,
		"火１":      {{"火", 1, ""}},
		"その他":     nil,
		"水1,水2後半": {{"水", 1, ""}, {"水", 2, "後半"}},
	}
	for in, want := range cases {
		if got := ParseSlots(in); !reflect.DeepEqual(got, want) {
			t.Errorf("ParseSlots(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestResolveOption(t *testing.T) {
	fac := []Option{{"NULL", "-----"}, {"00", "共通教育等科目"}, {"22", "理学部"}, {"25", "工学部"}, {"6800", "理工学研究科"}}
	term := []Option{{"0", "------"}, {"1", "前学期"}, {"2", "後学期"}, {"3", "通年"}}
	day := []Option{{"NULL", "---"}, {"1", "月曜日"}, {"8", "集中"}}
	period := []Option{{"NULL", "---"}, {"3", "３時限"}}
	cases := []struct {
		opts  []Option
		in    string
		want  string
		fails bool
	}{
		{fac, "25", "25", false},
		{fac, "工学部", "25", false},
		{fac, "共通教育", "00", false},
		{fac, "理工", "6800", false},
		{fac, "文学部", "", true},
		{term, "後期", "2", false},
		{term, "通年", "3", false},
		{day, "月", "1", false},
		{day, "集中", "8", false},
		{period, "3", "3", false},
		{period, "3限", "3", false},
	}
	for _, c := range cases {
		got, err := resolveOption(c.opts, c.in, optionAliases)
		if (err != nil) != c.fails || got != c.want {
			t.Errorf("resolveOption(%q) = %q, %v", c.in, got, err)
		}
	}
}

func TestAcademicYear(t *testing.T) {
	if y := AcademicYear(time.Date(2027, 3, 31, 0, 0, 0, 0, time.Local)); y != 2026 {
		t.Errorf("3月 = %d", y)
	}
	if y := AcademicYear(time.Date(2027, 4, 1, 0, 0, 0, 0, time.Local)); y != 2027 {
		t.Errorf("4月 = %d", y)
	}
}

func TestFormValuesKeepsSelectedAndHidden(t *testing.T) {
	p, _ := parseHTML("https://h/portal/Public/Syllabus/", `<form action="./x.aspx">
<input type="hidden" name="__VIEWSTATE" value="vs">
<select name="s"><option value="a">A</option><option selected="selected" value="b">B</option></select>
<select name="t"><option value="1">1</option></select>
<input name="q" type="text" value="hello">
<input type="submit" name="btn" value="go"></form>`)
	v := p.formValues()
	if v.Get("__VIEWSTATE") != "vs" || v.Get("s") != "b" || v.Get("t") != "1" || v.Get("q") != "hello" || v.Has("btn") {
		t.Errorf("formValues = %v", v)
	}
	if a := p.action(); a != "https://h/portal/Public/Syllabus/x.aspx" {
		t.Errorf("action = %q", a)
	}
}

func TestParseStudentProfile(t *testing.T) {
	field := func(name, val string) string {
		return `<span id="ctl00_phContents_ucForm_tr` + name + `_ttl_lbl">x</span><span id="ctl00_phContents_ucForm_tr` + name + `_lbl_lbl">` + val + `</span>`
	}
	p, _ := parseHTML("x", "<html><body>"+field("EnterYear", "２０２４")+field("FacName", "工学部")+field("DptName", "工学科")+
		field("DivName", "知能情報コース")+field("Grad", "３年後期")+field("Name", "秘密")+"</body></html>")
	got := parseStudentProfile(p)
	want := StudentProfile{EnterYear: 2024, Faculty: "工学部", Department: "工学科", Course: "知能情報コース", Grade: "３年後期"}
	if *got != want {
		t.Errorf("got %+v, want %+v", *got, want)
	}
}

func TestSearchResultsNeverNil(t *testing.T) {
	p, _ := parseHTML("x", "<html><body></body></html>")
	if got := parseSearchResults(p, DefaultBaseURL, 2026); got == nil {
		t.Error("0 件で nil を返している（JSON で null になる）")
	}
}
