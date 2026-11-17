package reviewboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Error is a non-2xx answer from Review Board.
type Error struct {
	Status int
	Code   int
	Msg    string
	Body   string
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "reviewboard: %d", e.Status)
	switch {
	case e.Code == 103 || e.Status == 401:
		b.WriteString(" authentication failed: the token is invalid, expired, or revoked; regenerate it under My Account -> API Tokens")
	case e.Code == 101 || e.Status == 403:
		b.WriteString(" permission denied")
	case e.Code == 100 || e.Status == 404:
		b.WriteString(" not found")
	case e.Code == 105 || e.Status == 400:
		b.WriteString(" validation failed")
	case e.Code == 207:
		b.WriteString(" repository file not found: the diff does not match the repository path; check basedir and the repository configuration")
	case e.Code == 219:
		b.WriteString(" diff is empty")
	case e.Code == 220:
		b.WriteString(" diff is too big")
	case e.Code == 224:
		b.WriteString(" diff parse error: the file must be a unified diff")
	}
	if e.Msg != "" {
		b.WriteString(": " + e.Msg)
	} else if s := snippet(e.Body); s != "" && e.Status != 401 {
		b.WriteString(": " + s)
	}
	return b.String()
}

// IsStatus reports whether err is a Review Board error with the given HTTP status.
func IsStatus(err error, status int) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == status
}

// IsCode reports whether err is a Review Board error with the given API error code.
func IsCode(err error, code int) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

func parseError(status int, body []byte) *Error {
	e := &Error{Status: status, Body: string(body)}
	var payload struct {
		Stat string `json:"stat"`
		Err  struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		} `json:"err"`
	}
	if json.Unmarshal(body, &payload) == nil {
		e.Code = payload.Err.Code
		e.Msg = payload.Err.Msg
	}
	return e
}

// snippet keeps an unexpected HTML/error body readable on one short line.
func snippet(body string) string {
	s := strings.TrimSpace(body)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "<") {
		var b strings.Builder
		inTag := false
		for _, r := range s {
			switch {
			case r == '<':
				inTag = true
			case r == '>':
				inTag = false
			case !inTag:
				b.WriteRune(r)
			}
		}
		s = b.String()
	}
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	if strings.HasPrefix(s, "{") {
		return "" // unparsed JSON is not useful to the model
	}
	return s
}
