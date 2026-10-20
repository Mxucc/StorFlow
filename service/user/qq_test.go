package user

import "testing"

func TestSanitizeQQEmailLocalPart(t *testing.T) {
	cases := map[string]string{
		"ABCDEF0123456789": "abcdef0123456789",
		"AbC-d.9_":         "abc-d.9_",
		"  spaced  ":       "spaced",
		"a+b@c":            "abc",
		"..":               "..",
		// 全部字符都不允许时退化为十六进制编码。
		"!!": "2121",
		// 非 ASCII 字符同样会被剔除，空格与变音符不保留。
		"unicode ünïcode": "unicodencode",
		"中文/不合法":          "e4b8ade696872fe4b88de59088e6b395",
		"":                "",
	}

	for input, expected := range cases {
		if got := sanitizeQQEmailLocalPart(input); got != expected {
			t.Errorf("sanitizeQQEmailLocalPart(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestQQErrorDetail(t *testing.T) {
	if got := qqErrorDetail(nil, "boom"); got != "boom" {
		t.Errorf("qqErrorDetail(nil, boom) = %q, want %q", got, "boom")
	}
	if got := qqErrorDetail(nil, ""); got != "no error detail returned" {
		t.Errorf("qqErrorDetail(nil, empty) = %q, want %q", got, "no error detail returned")
	}
	if got := qqErrorDetail([]byte("null"), ""); got != "no error detail returned" {
		t.Errorf("qqErrorDetail(null, empty) = %q, want %q", got, "no error detail returned")
	}
	if got := qqErrorDetail([]byte(`100019`), "invalid code"); got != "100019: invalid code" {
		t.Errorf("qqErrorDetail(100019, invalid code) = %q, want %q", got, "100019: invalid code")
	}
	if got := qqErrorDetail([]byte(`"100019"`), ""); got != "100019" {
		t.Errorf("qqErrorDetail(\"100019\", empty) = %q, want %q", got, "100019")
	}
}

func TestQQTruncate(t *testing.T) {
	if got := qqTruncate("  short  "); got != "short" {
		t.Errorf("qqTruncate(short) = %q, want %q", got, "short")
	}

	long := make([]byte, 300)
	for i := range long {
		long[i] = 'a'
	}
	got := qqTruncate(string(long))
	if len(got) != 259 || got[256:] != "..." {
		t.Errorf("qqTruncate(long) length = %d, tail = %q", len(got), got[250:])
	}
}
