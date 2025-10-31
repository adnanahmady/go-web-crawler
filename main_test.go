package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractLinks(t *testing.T) {
	// Setup a mock HTTP server to serve test HTML
	testHTML := `
	<html>
		<body>
			<a href="/internal/path">Internal Link 1</a>
			<a href="another/internal/path">Internal Link 2 (relative)</a>
			<a href="https://go.dev/external">External Link</a>
			<a href="/internal/path#fragment">Internal with fragment</a>
			<a href="/internal/path?query=1">Internal with query</a>
			<a href="http://another.example.com/path">Another External</a>
			<a href="ftp://ftp.example.com/file">Non-HTTP</a>
			<a>No href</a>
		</body>
	</html>
	`
	fn := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, testHTML)
	}
	handler := http.HandlerFunc(fn)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	baseURL := strings.Split(ts.URL, "://")[0] + "://" + strings.Split(ts.URL, "/")[2]
	links := extractLinks(ts.Client(), ts.URL, baseURL)

	expectedLinks := []string{
		ts.URL + "/internal/path",
		ts.URL + "/another/internal/path",
		ts.URL + "/internal/path#fragment",
		ts.URL + "/internal/path?query=1",
	}

	assertEqualLinks(t, len(links), len(expectedLinks))

	// Basic check, could be more robust using a map for comparison
	for _, expected := range expectedLinks {
		found := false
		for _, actual := range links {
			if actual == expected {
				found = true
				break
			}
		}
		assertFalsef(t, found, "Expected link %s not found in results: %v", expected, links)
	}
}

func assertFalsef(t testing.TB, cond bool, msg string, params ...any) {
	t.Helper()

	if !cond {
		t.Errorf(msg, params...)
	}
}

func assertEqualLinks[T comparable](t testing.TB, given, exp T) {
	t.Helper()

	if given != exp {
		t.Errorf("Expected %v links, got %v", given, exp)
	}
}
