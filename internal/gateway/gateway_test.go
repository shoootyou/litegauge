/**
 * @spec-handoff
 * @interface New(base *url.URL, rt http.RoundTripper, t Timing) *Client; Timing{Attempt, Pace, Retry time.Duration; InFlight int}; ProdTiming Timing
 * @behavior
 *   - ProdTiming is Timing{Attempt: 30s, Pace: 500ms, Retry: 1s, InFlight: 2}
 *   - The default transport offers HTTP/1.1 only: no "h2" in NextProtos, and a TLS server that offers h2 sees ProtoMajor 1
 *   - An http base never goes through a proxy (the key would travel in clear); an https base keeps the proxy function
 * @edge-cases
 *   - White-box: the tests read the private field client (*http.Client) and call the private method
 *     do(ctx, method, path, header, body, limit) whose result has fields status and err; keep these names or move the tests with them
 * @see ../cli/security_test.go
 */

package gateway

import (
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"
)

const testKey = "sk-FAKE-Zq7Xk9Rt2LmVwf4k3"

var testTiming = Timing{Attempt: 3 * time.Second, Pace: 0, Retry: 5 * time.Millisecond, InFlight: 2}

func TestM4_ProdTimingHoldsTheProductionValues(t *testing.T) {
	want := Timing{Attempt: 30 * time.Second, Pace: 500 * time.Millisecond, Retry: time.Second, InFlight: 2}
	if ProdTiming != want {
		t.Errorf("ProdTiming = %+v, want %+v", ProdTiming, want)
	}
}

func TestS5_OnlyHTTP11IsNegotiatedAgainstAnH2OfferingServer(t *testing.T) {
	var mu sync.Mutex
	var protos []int
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		protos = append(protos, r.ProtoMajor)
		mu.Unlock()
		_, _ = w.Write([]byte("{}"))
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	gw := New(base, nil, testTiming)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	tr := gw.client.Transport.(*http.Transport)
	if tr.TLSClientConfig == nil {
		t.Fatal("the production transport has no TLSClientConfig to extend")
	}
	tr.TLSClientConfig.RootCAs = pool
	if slices.Contains(tr.TLSClientConfig.NextProtos, "h2") {
		t.Errorf("NextProtos = %v offers h2", tr.TLSClientConfig.NextProtos)
	}
	raw := gw.do(t.Context(), http.MethodGet, "/v1/models", http.Header{"Authorization": {"Bearer " + testKey}}, nil, 1<<20)
	if raw.err != nil || raw.status != 200 {
		t.Fatalf("request failed: status=%d err=%v", raw.status, raw.err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(protos, []int{1}) {
		t.Errorf("server saw ProtoMajor %v, want [1]", protos)
	}
}

func transportOf(t *testing.T, base string) *http.Transport {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := New(u, nil, testTiming).client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("the default transport is not an *http.Transport")
	}
	return tr
}

func TestS10_HTTPBaseNeverGoesThroughAProxy(t *testing.T) {
	if transportOf(t, "http://localhost:9").Proxy != nil {
		t.Error("an http base must not use a proxy: it would receive the key in clear")
	}
}

func TestS10_HTTPSBaseHonoursTheProxyVariables(t *testing.T) {
	if transportOf(t, "https://gw.example.com").Proxy == nil {
		t.Error("an https base must keep the proxy function")
	}
}
