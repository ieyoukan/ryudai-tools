package ui

import "testing"

func TestIsWebClassURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://webclass.cc.u-ryukyu.ac.jp/webclass/download.php/a.pdf?file=x": true,
		"https://webclass.cc.u-ryukyu.ac.jp.example.com/webclass/a.pdf":         false,
		"https://user@webclass.cc.u-ryukyu.ac.jp/webclass/a.pdf":                false,
		"http://webclass.cc.u-ryukyu.ac.jp/webclass/a.pdf":                      false,
		"https://example.com/?https://webclass.cc.u-ryukyu.ac.jp/":              false,
	} {
		if got := isWebClassURL(raw); got != want {
			t.Errorf("isWebClassURL(%q) = %v, want %v", raw, got, want)
		}
	}
}
