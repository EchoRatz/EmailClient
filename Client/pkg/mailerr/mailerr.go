package mailerr

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"syscall"

	"email-client/pkg/pop3"
)

type Kind string

const (
	InvalidInput Kind = "invalid_input"
	Unreachable  Kind = "unreachable"
	Timeout      Kind = "timeout"
	AuthFailed   Kind = "auth_failed"
	Rejected     Kind = "rejected"
	Unexpected   Kind = "unexpected_response"
	Interrupted  Kind = "interrupted"
	Unknown      Kind = "unknown"
)

// InputError is a problem with what the user typed. Its message is shown as is.
type InputError struct{ msg string }

func (e *InputError) Error() string { return e.msg }

func Input(format string, args ...any) error {
	return &InputError{msg: fmt.Sprintf(format, args...)}
}

// Classify maps an error from any layer to one failure kind.
// Order matters: a timeout is also a net.Error, an auth failure is also a -ERR.
func Classify(err error) Kind {
	var (
		ie   *InputError
		pe   *pop3.Error
		te   *textproto.Error
		pre  textproto.ProtocolError
		ne   net.Error
		dnse *net.DNSError
	)
	switch {
	case err == nil:
		return ""
	case errors.As(err, &ie):
		return InvalidInput
	case errors.As(err, &pe) && (pe.Cmd == "USER" || pe.Cmd == "PASS"):
		return AuthFailed
	case errors.As(err, &pe), errors.As(err, &te):
		return Rejected
	case errors.As(err, &ne) && ne.Timeout():
		return Timeout
	case errors.Is(err, syscall.ECONNREFUSED), errors.Is(err, syscall.EHOSTUNREACH),
		errors.Is(err, syscall.ENETUNREACH), errors.As(err, &dnse):
		return Unreachable
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		return Interrupted
	case errors.Is(err, pop3.ErrUnexpectedResponse), errors.As(err, &pre):
		return Unexpected
	default:
		return Unknown
	}
}

var headlines = map[Kind]string{
	Unreachable: "Cannot reach the mail server. Is it running, and are the address and port right?",
	Timeout:     "The mail server did not answer in time.",
	AuthFailed:  "Login failed: wrong username or password.",
	Rejected:    "The mail server refused the request.",
	Unexpected:  "The mail server sent a reply this client does not understand.",
	Interrupted: "The connection was lost in the middle of a transfer.",
}

// Explain returns a short message for the user and the technical detail.
// detail is empty when the headline already says everything.
func Explain(err error) (kind Kind, headline, detail string) {
	kind = Classify(err)
	if h, ok := headlines[kind]; ok {
		return kind, h, err.Error()
	}
	return kind, err.Error(), ""
}
