package dev

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// busy is a website whose pages go on working after they load: /late
// fetches for a second and a half, and /polling fetches every 200 ms for
// good.
func busy(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/late", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><script>
			window.state = "loading";
			fetch("/slow").then(() => { window.state = "done"; });
		</script>`)
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1500 * time.Millisecond)
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("/polling", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><script>setInterval(() => fetch("/ping"), 200);</script>`)
	})
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "pong") })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestToolsWaitForThePageToSettle(t *testing.T) {
	srv := busy(t)
	e := newEngine(t)

	t.Run("a page still fetching after it loads", func(t *testing.T) {
		svc := load(t, `export default {
  name: "late",
  title: "Late",
  url: "URL/late",
  tools: {
    state: { description: "What the page has done.", async run() { return { state: (window as any).state ?? null } } },
  },
}
`, srv.URL)
		if got := call(t, e, svc, "state", "{}"); got["state"] != "done" {
			t.Errorf("the tool ran while the page was still fetching: %v", got)
		}
	})

	t.Run("a page that never goes quiet", func(t *testing.T) {
		svc := load(t, `export default {
  name: "polling",
  title: "Polling",
  url: "URL/polling",
  tools: {
    state: { description: "What the page has done.", async run() { return { state: "ran" } } },
  },
}
`, srv.URL)
		start := time.Now()
		if _, err := e.Call(context.Background(), svc, "state", nil); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(start); took > 12*time.Second {
			t.Errorf("the call took %v; a page that never goes quiet should cost about 5 s", took)
		}
	})
}
