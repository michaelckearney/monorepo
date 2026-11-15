package e2e

import "testing"

// The gallery holds one PNG per test and subtest that called Open, named
// from the test's name with each subtest separator an underscore.
func TestGalleryFileNames(t *testing.T) {
	for _, tc := range []struct{ test, png string }{
		{"TestSettings", "TestSettings"},
		{"TestSettings/empty", "TestSettings_empty"},
		{"TestChats/open/a conversation", "TestChats_open_a_conversation"},
		{"TestSign in/empty list!", "TestSign_in_empty_list"},
		{"TestA//B", "TestA_B"},
		{"TestA/v1.2", "TestA_v1_2"},
	} {
		if got := galleryName(tc.test); got != tc.png {
			t.Errorf("galleryName(%q) = %q, want %q", tc.test, got, tc.png)
		}
	}
}
