package security

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestRedactErrorPreservesDiagnosticsAndHandlesEscapedCredentials(t *testing.T) {
	const secret = "12345:token/value+test"
	for _, value := range []string{secret, url.PathEscape(secret), url.QueryEscape(secret)} {
		err := RedactError(errors.New("Post https://example.test/bot"+value+"/sendMessage: connection refused"), secret)
		if strings.Contains(err.Error(), value) || !strings.Contains(err.Error(), "<redacted>") || !strings.Contains(err.Error(), "connection refused") {
			t.Fatalf("incorrect redaction: %v", err)
		}
	}
	if RedactError(nil, secret) != nil {
		t.Fatal("nil became an error")
	}
	err := errors.New("unrelated failure")
	if got := RedactError(err, ""); got != err {
		t.Fatal("empty secret changed the error")
	}
}
