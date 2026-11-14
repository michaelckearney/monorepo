package payroll

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/apps/northwind"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/internal/fake"
)

// setup signs alex in, with Northwind ID faked, and returns a browser and
// an access token.
func setup(t *testing.T) (*fake.Browser, string, *bool) {
	signedIn := true
	p := newPayroll(fake.Store(t), http.DefaultClient)
	p.signedIn = func(context.Context, *http.Request) (bool, error) { return signedIn, nil }
	p.exchangeCode = func(_ context.Context, form url.Values, client, secret string) (northwind.User, error) {
		if client != clientID || secret != "payday-client-secret" || form.Get("code") != "good-code" {
			t.Errorf("exchange: %s %v", client, form)
		}
		return northwind.User{Username: "alex"}, nil
	}
	b := fake.New(t, hosts.Payroll, p.handler())
	b.Client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	login := b.Get("/auth/login")
	to, _ := url.Parse(login.Header.Get("Location"))
	if to.Host != hosts.NorthwindID+":"+hosts.Port || to.Query().Get("client_id") != "payday" {
		t.Fatalf("not sent to Northwind ID: %s", to)
	}
	b.Get("/auth/callback?code=good-code&state=" + to.Query().Get("state"))
	r := b.Do("POST", "/api/auth/refresh", nil, nil)
	var tok struct{ AccessToken string }
	json.Unmarshal([]byte(r.Body), &tok)
	if tok.AccessToken == "" {
		t.Fatalf("refresh: %d %s", r.Status, r.Body)
	}
	return b, tok.AccessToken, &signedIn
}

func unpack(t *testing.T, body string) []map[string]any {
	z, err := gzip.NewReader(base64.NewDecoder(base64.StdEncoding, strings.NewReader(body)))
	if err != nil {
		t.Fatalf("not packed: %q", body)
	}
	raw, _ := io.ReadAll(z)
	var out []map[string]any
	json.Unmarshal(raw, &out)
	return out
}

func send(t *testing.T, b *fake.Browser, token, batch string) []map[string]any {
	p := b.JSON("POST", "/rpc?batch=1", batch, http.Header{"Authorization": {"Bearer " + token}})
	if p.Header.Get("Content-Type") != packedType {
		t.Fatalf("%d %s", p.Status, p.Body)
	}
	return unpack(t, p.Body)
}

func TestBatchedPackedRPC(t *testing.T) {
	b, token, _ := setup(t)
	out := send(t, b, token, `[{"id":1,"method":"me.get"},{"id":2,"method":"payslips.list","params":{"year":2026}},{"id":3,"method":"nope"}]`)
	if len(out) != 3 || out[0]["result"].(map[string]any)["name"] != "Alex Morgan" {
		t.Fatalf("%v", out)
	}
	if slips := out[1]["result"].([]any); len(slips) != 19 {
		t.Errorf("payslips: %d", len(slips))
	}
	if out[2]["error"].(map[string]any)["code"] != "method_not_found" {
		t.Errorf("unknown method: %v", out[2])
	}
	pdf := b.Do("GET", "/payslips/PS-2026-09-18.pdf", nil, http.Header{"Authorization": {"Bearer " + token}})
	if !bytes.HasPrefix([]byte(pdf.Body), []byte("%PDF")) {
		t.Errorf("payslip PDF: %d", pdf.Status)
	}
}

func TestBankDetailsNeedThePasswordAgain(t *testing.T) {
	b, token, _ := setup(t)
	update := `[{"id":1,"method":"bank.update","params":{"bankName":"Harbor CU","routingNumber":"123456789","accountNumber":"99887766","accountType":"savings"}}]`
	out := send(t, b, token, update)
	e := out[0]["error"].(map[string]any)
	if e["code"] != "reauth_required" {
		t.Fatalf("%v", out)
	}
	login := b.Get(strings.TrimPrefix(e["data"].(map[string]any)["url"].(string), hosts.URL(hosts.Payroll)))
	to, _ := url.Parse(login.Header.Get("Location"))
	if to.Query().Get("prompt") != "login" {
		t.Fatalf("re-auth does not ask for the password: %s", to)
	}
	b.Get("/auth/callback?code=good-code&state=" + to.Query().Get("state"))
	if out := send(t, b, token, update); out[0]["result"] == nil {
		t.Errorf("after the password: %v", out)
	}
}

func TestSigningOutOfNorthwindIDEndsTheSession(t *testing.T) {
	b, _, signedIn := setup(t)
	*signedIn = false
	if p := b.Do("POST", "/api/auth/refresh", nil, nil); p.Status != 401 || !strings.Contains(p.Body, "signed_out") {
		t.Errorf("%d %s", p.Status, p.Body)
	}
}
