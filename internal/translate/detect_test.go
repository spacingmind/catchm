package translate

import "testing"

// TestDetectLanguageBetweenTwoOptions feeds clearly unambiguous English
// and Vietnamese samples through the two-way detector with the pair
// ("en", "vi"), in both argument orders; each sample must be attributed
// to its own language regardless of which candidate is listed first.
func TestDetectLanguageBetweenTwoOptions(t *testing.T) {
	samples := []struct {
		text string
		want string
	}{
		{"The quick brown fox jumps over the lazy dog near the river bank", "en"},
		{"Xin chào, tôi rất vui được gặp bạn ở đây hôm nay", "vi"},
		{"Could you walk me through the deployment plan one more time", "en"},
		{"Tôi muốn hỏi về kế hoạch triển khai của dự án năm nay", "vi"},
	}
	for _, s := range samples {
		for _, pair := range [][2]string{{"en", "vi"}, {"vi", "en"}} {
			if got := DetectLanguage(s.text, pair[0], pair[1]); got != s.want {
				t.Errorf("DetectLanguage(%q, %q, %q) = %q, want %q", s.text, pair[0], pair[1], got, s.want)
			}
		}
	}
}

// TestDetectLanguageFallsBackToOne covers the degenerate inputs where
// detection cannot distinguish anything: empty or unknown codes resolve
// to the first usable candidate rather than failing.
func TestDetectLanguageFallsBackToOne(t *testing.T) {
	if got := DetectLanguage("hello", "", "vi"); got != "vi" {
		t.Errorf("empty first code = %q, want vi", got)
	}
	if got := DetectLanguage("hello", "en", ""); got != "en" {
		t.Errorf("empty second code = %q, want en", got)
	}
	if got := DetectLanguage("hello", "en", "en"); got != "en" {
		t.Errorf("identical codes = %q, want en", got)
	}
	if got := DetectLanguage("hello", "en", "de"); got != "en" {
		t.Errorf("unknown code pair = %q, want en (first candidate)", got)
	}
}
