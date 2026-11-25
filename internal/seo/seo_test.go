package seo

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicPagesAreIndexedAndPrivatePagesAreNot(t *testing.T) {
	for path, want := range map[string]bool{
		"/": true, "/login": true, "/syllabus": true, "/downloads": true, "/terms": true, "/privacy": true,
		"/courses": false, "/grades": false, "/plan": false, "/settings": false, "/messages": false,
		"/syllabus/2026/990000001": false, "/lectures/2026/990000001": false, "/no-such-page": false,
	} {
		if got := ForPath(path).Index; got != want {
			t.Errorf("ForPath(%q).Index = %v, want %v", path, got, want)
		}
	}
}

func TestHead(t *testing.T) {
	base := "https://ryudai-tools.example.com"
	login := Head(ForPath("/"), base, "/")
	for _, want := range []string{
		"<title>Ryudai Tools - 琉大生のための非公式の大学生活支援ツール</title>",
		`<link rel="canonical" href="https://ryudai-tools.example.com/login" />`,
		`<meta property="og:image" content="https://ryudai-tools.example.com/brand/logo-512.png" />`,
		`"@type":"WebApplication"`,
		`<meta name="description" content="琉球大学の WebClass`,
	} {
		if !strings.Contains(login, want) {
			t.Errorf("login head lacks %s\n%s", want, login)
		}
	}
	private := Head(ForPath("/grades"), base, "/grades")
	if !strings.Contains(private, `content="noindex, nofollow"`) || strings.Contains(private, "canonical") {
		t.Errorf("private head = %s", private)
	}
	escaped := Head(Page{Title: `a"<b>`, Description: "x"}, base, "/x")
	if strings.Contains(escaped, `a"<b>`) {
		t.Errorf("title was not escaped: %s", escaped)
	}
}

func TestRobotsAndSitemap(t *testing.T) {
	base := "https://ryudai-tools.example.com"
	robots := RobotsTxt(base)
	for _, want := range []string{"Disallow: /courses", "Disallow: /api/", "Disallow: /syllabus/", "Sitemap: https://ryudai-tools.example.com/sitemap.xml"} {
		if !strings.Contains(robots, want) {
			t.Errorf("robots.txt lacks %q:\n%s", want, robots)
		}
	}
	if strings.Contains(robots, "Disallow: /syllabus\n") {
		t.Error("syllabus search page is blocked")
	}
	sitemap := SitemapXML(base)
	if !strings.Contains(sitemap, "<loc>https://ryudai-tools.example.com/login</loc>") || strings.Contains(sitemap, "/courses") {
		t.Errorf("sitemap = %s", sitemap)
	}
}

func TestBaseURL(t *testing.T) {
	r := httptest.NewRequest("GET", "http://localhost:8080/login", nil)
	if got := BaseURL(r, ""); got != "http://localhost:8080" {
		t.Errorf("BaseURL = %q", got)
	}
	r.Header.Set("X-Forwarded-Proto", "https")
	if got := BaseURL(r, ""); got != "https://localhost:8080" {
		t.Errorf("BaseURL behind proxy = %q", got)
	}
	if got := BaseURL(r, "https://ryudai-tools.example.com/"); got != "https://ryudai-tools.example.com" {
		t.Errorf("configured BaseURL = %q", got)
	}
}
