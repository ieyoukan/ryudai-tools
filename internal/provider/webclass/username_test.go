package webclass

import (
	"errors"
	"testing"
)

func TestNormalizeUsername(t *testing.T) {
	for input, want := range map[string]string{
		"e000001":                   "e000001",
		"  e000001 ":                "e000001",
		"ｅ０００００１":                   "e000001", // 全角で入力
		"　e000001　":                 "e000001", // 全角の空白
		"e000001@u-ryukyu.ac.jp":    "e000001",
		"e000001@cs.u-ryukyu.ac.jp": "e000001",
		"e000001@ie.u-ryukyu.ac.jp": "e000001",
		"e000001@CS.U-RYUKYU.AC.JP": "e000001",
		"ｅ０００００１＠ｃｓ．ｕ－ｒｙｕｋｙｕ．ａｃ．ｊｐ": "e000001",
		"": "",
	} {
		got, err := NormalizeUsername(input)
		if err != nil || got != want {
			t.Errorf("NormalizeUsername(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{
		"student@gmail.com",
		"e000001@u-ryukyu.ac.jp.example.com",
		"e000001@evil-u-ryukyu.ac.jp",
		"@cs.u-ryukyu.ac.jp",
		"a@b@cs.u-ryukyu.ac.jp",
	} {
		if got, err := NormalizeUsername(input); !errors.Is(err, ErrEmailAsUsername) {
			t.Errorf("NormalizeUsername(%q) = %q, %v; want ErrEmailAsUsername", input, got, err)
		}
	}
}
