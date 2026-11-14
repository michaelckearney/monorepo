package photos

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/internal/fake"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

func TestSignedUploadsAndPresignedDownloads(t *testing.T) {
	app, objects := New(fake.Store(t))
	b := fake.New(t, hosts.Photos, app)
	b.Serve(hosts.PhotoFiles, objects)
	if p := b.JSON("POST", "/api/login/password", `{"email":"alex@example.localhost","password":"`+site.Password+`"}`, nil); p.Status != 200 {
		t.Fatalf("login: %s", p.Body)
	}
	var s struct{ SigningKey string }
	json.Unmarshal([]byte(b.Get("/api/session").Body), &s)

	var dl struct{ URL string }
	json.Unmarshal([]byte(b.Get("/api/photos/ph-001/download").Body), &dl)
	if p := b.Get(dl.URL); p.Status != 200 || !strings.HasPrefix(p.Body, "<svg") {
		t.Fatalf("download: %d %s", p.Status, p.Body)
	}
	if p := b.Get(strings.Replace(dl.URL, "signature=", "signature=0", 1)); p.Status != 403 {
		t.Errorf("a changed signature: %d", p.Status)
	}

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("file", "beach.svg")
	part.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`))
	form.Close()
	path := "/api/albums/al-coast/photos"
	header := http.Header{"Content-Type": {form.FormDataContentType()}}
	if p := b.Do("POST", path, bytes.NewReader(body.Bytes()), header); p.Status != 401 || !strings.Contains(p.Body, "missing_signature") {
		t.Errorf("unsigned: %d %s", p.Status, p.Body)
	}
	date := time.Now().UTC().Format(time.RFC3339)
	header.Set("X-Shoebox-Date", date)
	header.Set("X-Shoebox-Signature", site.HMAC([]byte(s.SigningKey), "POST\n"+path+"\n"+date))
	if p := b.Do("POST", path, bytes.NewReader(body.Bytes()), header); p.Status != 201 || !strings.Contains(p.Body, "beach.svg") {
		t.Errorf("signed: %d %s", p.Status, p.Body)
	}
}
