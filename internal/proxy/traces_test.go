package proxy

import (
	"net/http"
	"strings"
	"testing"
)

func TestSanitizeResponseBodyRedactsKnownSecretInMessage(t *testing.T) {
	t.Parallel()
	const secret = "sk-group-secret-1234"
	body := []byte(`{"error":{"message":"invalid Bearer sk-group-secret-1234"}}`)

	got := sanitizeResponseBody("application/json", body, secret)
	if strings.Contains(got, secret) {
		t.Fatalf("sanitized response contains secret: %q", got)
	}
	if !strings.Contains(got, "<redacted>") {
		t.Fatalf("sanitized response = %q, want redaction marker", got)
	}
}

func TestSanitizeResponseBodyRedactsSecretBeforeJSONEscaping(t *testing.T) {
	t.Parallel()
	const secret = "sk-<group>&secret"
	body := []byte(`{"error":{"message":"invalid sk-<group>&secret"}}`)
	got := sanitizeResponseBody("application/json", body, secret)
	if strings.Contains(got, secret) || strings.Contains(got, `sk-\u003cgroup\u003e\u0026secret`) {
		t.Fatalf("sanitized response contains secret: %q", got)
	}
	if !strings.Contains(got, "<redacted>") {
		t.Fatalf("sanitized response = %q, want redaction marker", got)
	}
}

func TestSanitizeHeaderMapRedactsProviderCustomHeaderValues(t *testing.T) {
	t.Parallel()
	const secret = "custom-header-secret-1234"
	header := http.Header{
		"X-Token": []string{secret},
		"X-Echo":  []string{"prefix " + secret + " suffix"},
	}

	got := sanitizeHeaderMap(header, secret)
	for key, value := range got {
		if strings.Contains(value, secret) {
			t.Fatalf("sanitized header %s contains secret: %q", key, value)
		}
	}
	if !strings.Contains(got["X-Token"], "<redacted>") || !strings.Contains(got["X-Echo"], "<redacted>") {
		t.Fatalf("sanitized headers = %#v, want redaction markers", got)
	}
}
