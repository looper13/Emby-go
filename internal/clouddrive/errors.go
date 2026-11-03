package clouddrive

import (
	"errors"
	"regexp"
	"strings"
)

var signedURL = regexp.MustCompile(`https?://[^\s"<>]+`)

// SafeError 隐藏凭据和临时直链，保留明确的鉴权、重试与文件缺失标记。
func SafeError(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
		}
	}
	message = signedURL.ReplaceAllString(message, "[URL]")
	if len(message) > 500 {
		message = message[:500]
	}
	e := &Error{Message: message, Retryable: true}
	var prior *Error
	if errors.As(err, &prior) {
		e.Auth, e.Retryable, e.Missing = prior.Auth, prior.Retryable, prior.Missing
		e.NotSubmitted = prior.NotSubmitted
	}
	return e
}
