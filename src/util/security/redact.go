package security

import (
	"errors"
	"net/url"
	"strings"
)

// RedactError removes credentials embedded in outbound client errors before
// they reach logs or API responses. Keep the failure's diagnostic text.
func RedactError(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}
	message := err.Error()
	for _, value := range []string{secret, url.PathEscape(secret), url.QueryEscape(secret)} {
		message = strings.ReplaceAll(message, value, "<redacted>")
	}
	return errors.New(message)
}
