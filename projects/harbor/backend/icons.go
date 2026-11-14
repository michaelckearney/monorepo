package backend

import (
	"context"
	"database/sql"
	"net/http"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/favicon"
)

const (
	// iconRefresh is how long a found icon is trusted. Sites rarely change
	// theirs, and a refresh is a conditional request that is usually a 304.
	iconRefresh = 14 * 24 * time.Hour
	// Failures back off from half an hour to a day, so a service on a VPN you
	// are not connected to is not tried on every sweep.
	iconRetryBase = 30 * time.Minute
	iconRetryMax  = 24 * time.Hour
	// iconForgetAfter is how long the icon of a site no service uses is kept,
	// so editing a URL back and forth does not throw it away.
	iconForgetAfter = 30 * 24 * time.Hour
	// iconFirstSweep keeps the first sweep out of the way of the window
	// opening.
	iconFirstSweep = 3 * time.Second
	// iconSweepEvery looks again while Harbor runs, so a site that comes up
	// later, such as the fixtures or a VPN connecting, gets its icon. A sweep
	// skips origins whose backoff or refresh has not passed without touching
	// the network.
	iconSweepEvery = 5 * time.Minute
	// iconBudget bounds the work on one origin, across all its candidates;
	// iconRequestTimeout bounds each request.
	iconBudget         = 15 * time.Second
	iconRequestTimeout = 5 * time.Second
	iconWorkers        = 4
)

// Icons keeps service_icons in step with the services' URLs. The frontend
// reads the table with a live query (icons.ts).
type Icons struct {
	db     *sql.DB
	client *http.Client
	// requests holds one pending sweep: saving three services at once costs
	// one sweep, and two sweeps never race to write the same origin.
	requests chan struct{}
	stop     context.CancelFunc
}

func newIcons(db *sql.DB) *Icons {
	client := favicon.NewClient()
	client.Timeout = iconRequestTimeout
	return &Icons{db: db, client: client, requests: make(chan struct{}, 1)}
}

// start runs the one goroutine that fetches icons, until ServiceShutdown.
func (i *Icons) start() {
	ctx, stop := context.WithCancel(context.Background())
	i.stop = stop
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(iconFirstSweep):
		}
		i.request()
		ticker := time.NewTicker(iconSweepEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				i.request()
			case <-i.requests:
				i.sweep(ctx)
			}
		}
	}()
}

// ServiceShutdown stops the sweeps when the app quits.
func (i *Icons) ServiceShutdown() error {
	i.stop()
	return nil
}

// request asks for a sweep without waiting. One already pending is enough.
func (i *Icons) request() {
	select {
	case i.requests <- struct{}{}:
	default:
	}
}

// iconState is what a sweep needs of an origin's row: when to try again,
// and the validators that make the refresh conditional.
type iconState struct {
	next     time.Time
	failures int
	prior    favicon.Conditional
}

// sweep fetches the icons that are due, and forgets ones no service has used
// for a while. Failures are quiet: an icon is decoration, and there is
// nothing you could do about one that will not download.
func (i *Icons) sweep(ctx context.Context) {
	origins, err := i.origins()
	if err != nil {
		return
	}
	states, err := i.states()
	if err != nil {
		return
	}
	now := time.Now().UTC()
	var due []string
	for _, origin := range origins {
		if s, ok := states[origin]; !ok || !now.Before(s.next) {
			due = append(due, origin)
		}
	}

	work := make(chan string)
	var wg sync.WaitGroup
	for range min(iconWorkers, len(due)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for origin := range work {
				i.fetch(ctx, origin, states[origin])
			}
		}()
	}
	for _, origin := range due {
		if ctx.Err() != nil {
			break
		}
		work <- origin
	}
	close(work)
	wg.Wait()

	// Even with nothing due: a service deleted leaves a row only this removes.
	i.forget(origins, now.Add(-iconForgetAfter))
}

// origins lists the distinct origins the services' URLs are on. A service
// whose source would not read has no URL.
func (i *Icons) origins() ([]string, error) {
	rows, err := i.db.Query(`SELECT json_extract(manifest, '$.url') FROM services WHERE manifest IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var origins []string
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, err
		}
		if origin := favicon.Origin(url); origin != "" && !seen[origin] {
			seen[origin] = true
			origins = append(origins, origin)
		}
	}
	return origins, rows.Err()
}

func (i *Icons) states() (map[string]iconState, error) {
	rows, err := i.db.Query(`
		SELECT origin, next_attempt_at, failure_count,
			coalesce(icon_url, ''), coalesce(etag, ''), coalesce(last_modified, ''),
			coalesce(dark_icon_url, ''), coalesce(dark_etag, ''), coalesce(dark_last_modified, '')
		FROM service_icons`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := map[string]iconState{}
	for rows.Next() {
		var origin, next string
		var s iconState
		l, d := &s.prior.Light, &s.prior.Dark
		if err := rows.Scan(&origin, &next, &s.failures, &l.IconURL, &l.ETag, &l.LastModified,
			&d.IconURL, &d.ETag, &d.LastModified); err != nil {
			return nil, err
		}
		if s.next, err = time.Parse(time.RFC3339, next); err != nil {
			return nil, err
		}
		states[origin] = s
	}
	return states, rows.Err()
}

func (i *Icons) fetch(ctx context.Context, origin string, prior iconState) {
	ctx, cancel := context.WithTimeout(ctx, iconBudget)
	defer cancel()
	result, notModified, err := favicon.Fetch(ctx, i.client, origin, prior.prior)
	now := time.Now().UTC()
	switch {
	case err != nil:
		i.failed(origin, now, now.Add(iconBackoff(prior.failures+1)), err)
	case notModified:
		i.db.Exec(`
			UPDATE service_icons SET fetched_at = ?, next_attempt_at = ?, failure_count = 0, last_error = NULL
			WHERE origin = ?`, stamp(now), stamp(now.Add(iconRefresh)), origin)
	default:
		i.save(origin, result, now)
	}
}

// save writes a fetch that worked, or found no icon. The dark columns are
// always written, nulls too, so a site that stops declaring a dark icon
// loses the one stored.
func (i *Icons) save(origin string, r favicon.Result, now time.Time) {
	dark := favicon.Icon{}
	if r.Dark != nil {
		dark = *r.Dark
	}
	i.db.Exec(`
		INSERT INTO service_icons (origin, status, icon_url, mime_type, bytes, etag, last_modified,
			dark_icon_url, dark_mime_type, dark_bytes, dark_etag, dark_last_modified,
			fetched_at, next_attempt_at, failure_count, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, NULL)
		ON CONFLICT (origin) DO UPDATE SET
			status = excluded.status, icon_url = excluded.icon_url, mime_type = excluded.mime_type,
			bytes = excluded.bytes, etag = excluded.etag, last_modified = excluded.last_modified,
			dark_icon_url = excluded.dark_icon_url, dark_mime_type = excluded.dark_mime_type,
			dark_bytes = excluded.dark_bytes, dark_etag = excluded.dark_etag,
			dark_last_modified = excluded.dark_last_modified, fetched_at = excluded.fetched_at,
			next_attempt_at = excluded.next_attempt_at, failure_count = 0, last_error = NULL`,
		origin, string(r.Status),
		null(r.Light.IconURL), null(r.Light.MIMEType), blob(r.Light.Bytes), null(r.Light.ETag), null(r.Light.LastModified),
		null(dark.IconURL), null(dark.MIMEType), blob(dark.Bytes), null(dark.ETag), null(dark.LastModified),
		stamp(now), stamp(now.Add(iconRefresh)))
}

// failed records a failed attempt. An icon found before is kept.
func (i *Icons) failed(origin string, now, next time.Time, err error) {
	i.db.Exec(`
		INSERT INTO service_icons (origin, status, fetched_at, next_attempt_at, failure_count, last_error)
		VALUES (?, 'error', ?, ?, 1, ?)
		ON CONFLICT (origin) DO UPDATE SET next_attempt_at = excluded.next_attempt_at,
			failure_count = service_icons.failure_count + 1, last_error = excluded.last_error`,
		origin, stamp(now), stamp(next), err.Error())
}

// forget deletes the icons of origins no service is on, untouched since before.
func (i *Icons) forget(keep []string, before time.Time) {
	rows, err := i.db.Query(`SELECT origin FROM service_icons WHERE fetched_at < ?`, stamp(before))
	if err != nil {
		return
	}
	kept := map[string]bool{}
	for _, origin := range keep {
		kept[origin] = true
	}
	var stale []string
	for rows.Next() {
		var origin string
		if rows.Scan(&origin) == nil && !kept[origin] {
			stale = append(stale, origin)
		}
	}
	rows.Close()
	for _, origin := range stale {
		i.db.Exec(`DELETE FROM service_icons WHERE origin = ?`, origin)
	}
}

// iconBackoff doubles from the base to the cap. failures counts the attempt
// just made, so the first failure waits the base.
func iconBackoff(failures int) time.Duration {
	backoff := iconRetryBase
	for range failures - 1 {
		backoff *= 2
		if backoff >= iconRetryMax {
			return iconRetryMax
		}
	}
	return backoff
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func null(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func blob(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
