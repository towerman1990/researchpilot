package envconfig

import "testing"

func TestString(t *testing.T) {
	t.Setenv("TEST_SETTING", "")
	if got := String("TEST_SETTING", "default"); got != "default" {
		t.Fatalf("empty setting returned %q", got)
	}
	t.Setenv("TEST_SETTING", "custom")
	if got := String("TEST_SETTING", "default"); got != "custom" {
		t.Fatalf("custom setting returned %q", got)
	}
}

func TestInt(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
	}{
		{"", 30}, {"invalid", 30}, {"0", 30}, {"1", 1}, {"10000", 10000}, {"10001", 30},
	} {
		t.Setenv("TEST_SETTING", test.value)
		if got := Int("TEST_SETTING", 30, 1, 10000); got != test.want {
			t.Errorf("Int(%q) = %d, want %d", test.value, got, test.want)
		}
	}
}
