package printer

import "testing"

func TestCleanStripsTerminalControls(t *testing.T) {
	cases := map[string]string{
		"plain":                 "plain",
		"سرور ۱":                "سرور ۱",
		"a\tb\nc":               "a\tb\nc",
		"web\x1b[2J\x1b[Hroot":  "web[2J[Hroot",
		"bell\x07back\x08space": "bellbackspace",
		"c1\u009b31mred":        "c131mred",
		"carriage\rreturn":      "carriagereturn",
		"\x1b]0;pwned\x07title": "]0;pwnedtitle",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}
