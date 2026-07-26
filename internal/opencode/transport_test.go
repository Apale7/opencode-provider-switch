package opencode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDoJSONRejectsRedirectBeforeForwardingCredentials(t *testing.T) {
	t.Parallel()
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer source.Close()

	req, err := http.NewRequest(http.MethodGet, source.URL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	req.Header.Set("X-Api-Key", "group-secret")
	resp, _, err := DoJSON(context.Background(), req, TransportOptions{})
	if err == nil {
		t.Fatal("DoJSON() error = nil, want redirect status error")
	}
	if resp == nil || resp.StatusCode != http.StatusFound {
		t.Fatalf("response = %#v, want 302", resp)
	}
	if redirected.Load() != 0 {
		t.Fatalf("redirect destination received %d request(s)", redirected.Load())
	}
}
