package favicon

import "testing"

// The frontend computes the same key with `new URL(x).origin`, and a mismatch
// would not fail loudly: every lookup would miss and no icon would ever render.
// frontend/src/__tests__/icons.test.ts asserts this same table.
func TestOrigin(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain host", "https://example.com", "https://example.com"},
		{"trailing slash", "https://example.com/", "https://example.com"},
		{"path query fragment dropped", "https://example.com/a/b?c=d#e", "https://example.com"},
		{"scheme and host lowercased", "HTTPS://Example.COM/x", "https://example.com"},
		{"default https port dropped", "https://example.com:443/x?y#z", "https://example.com"},
		{"default http port dropped", "http://example.com:80/", "http://example.com"},
		{"non-default port kept", "http://example.com:8080/path", "http://example.com:8080"},
		{"https non-default port kept", "https://gitea.internal:3000", "https://gitea.internal:3000"},
		{"userinfo dropped", "https://user:pw@example.com/x", "https://example.com"},
		{"ipv6 keeps brackets", "http://[::1]:8080/x", "http://[::1]:8080"},
		{"ipv6 default port dropped", "http://[::1]:80/", "http://[::1]"},
		{"localhost", "http://localhost:9245/app", "http://localhost:9245"},
		{"surrounding whitespace", "  https://example.com/x  ", "https://example.com"},
		{"missing scheme", "example.com", ""},
		{"scheme-relative", "//example.com", ""},
		{"non-http scheme", "mailto:someone@example.com", ""},
		{"file scheme", "file:///Users/x/index.html", ""},
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		// net/url and the WHATWG parser disagree about slash counts and about
		// internationalized hosts. These are the cases that forced Origin to
		// normalize rather than to parse straight through.
		{"one slash", "https:/example.com/x", "https://example.com"},
		{"no slashes", "https:example.com", "https://example.com"},
		{"three slashes", "https:///path", "https://path"},
		{"four slashes", "http:////a/b", "http://a"},
		{"underscore host kept", "https://exa_mple.com/", "https://exa_mple.com"},
		{"trailing dot kept", "https://EXAMPLE.com.", "https://example.com."},
		{"idn punycoded", "http://例え.jp/", "http://xn--r8jz45g.jp"},
		{"port zero kept", "https://example.com:0/", "https://example.com:0"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := Origin(testCase.in); got != testCase.want {
				t.Fatalf("Origin(%q) = %q, want %q", testCase.in, got, testCase.want)
			}
		})
	}
}
