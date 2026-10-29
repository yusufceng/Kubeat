package alert

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// failure is what an attempt the channel did not take says about the
// channel, and so what the engine does next (README "Alarm", the table of
// failure classes).
type failure int

const (
	// unreachable: the attempt did not reach a receiver that could take
	// it: a connection error, a timeout, a 5xx (but a 503 that says when to
	// come back), 407 or 408, and any failure that is not an HTTP answer
	// (e-mail among them). The tries of an attempt are made, a channel
	// software reads pauses, and the note waits 1, 2, 4… minutes.
	unreachable failure = iota
	// busy: 429, or 503 with a usable Retry-After. The receiver is there
	// and asked to be asked later: no more tries in the attempt; the note
	// goes again after Retry-After (retryBase without one, RetryMax at
	// most), a channel software reads pauses until then, and the wait does
	// not double. Not a refusal: the note keeps its place in the
	// channel's order, and one sent once goes once more.
	busy
	// refused: any other 4xx. The receiver is there and will not take
	// this notification, most likely because of how the channel or the
	// receiver is set up: no more tries in the attempt, the channel does
	// not pause and the notes behind it in the pass go, and the note goes
	// again RetryMax later while it is sent again at all.
	refused
)

func (f failure) String() string {
	switch f {
	case busy:
		return "busy"
	case refused:
		return "refused"
	}
	return "unreachable"
}

// LogValue logs the class by its name: the JSON handler the backend logs
// with (cmd/server) would write the number a failure is held as, which
// no log query for "refused" finds.
func (f failure) LogValue() slog.Value {
	return slog.StringValue(f.String())
}

// refusedAdvice follows the error of a refused attempt, on the channel's
// row, in the delivery log and in the log: the receiver answered, and
// trying again will not help until the channel or the receiver is fixed.
const refusedAdvice = " (refused by the receiver: fix the channel's address or key, or the receiver's settings)"

// classify says what a failed attempt says about the channel, at now: an
// answer of 429, or of 503 with a Retry-After in the future, is busy and
// comes with how long to wait (Retry-After, retryBase without one, RetryMax
// at most); one of 407 or 408 is unreachable, as is a failure without an
// answer, a 503 without a usable Retry-After and any other answer outside
// 4xx; any other 4xx is refused.
func (e *Engine) classify(err error, now time.Time) (failure, time.Duration) {
	var answer *statusError
	if !errors.As(err, &answer) {
		return unreachable, 0
	}
	after := retryAfter(answer.retryAfter, now)
	switch status := answer.status; {
	case status == http.StatusTooManyRequests, status == http.StatusServiceUnavailable && after > 0:
		if after <= 0 {
			after = retryBase
		}
		return busy, min(after, e.opts.RetryMax)
	case status == http.StatusProxyAuthRequired, status == http.StatusRequestTimeout:
		return unreachable, 0
	case status >= 400 && status < 500:
		return refused, 0
	}
	return unreachable, 0
}

// maxRetryAfter bounds a Retry-After in seconds before it becomes a
// duration, which a larger one would overflow; RetryMax bounds it anyway.
const maxRetryAfter = 1 << 30

// retryAfter reads a Retry-After header (RFC 9110, 10.2.3), a number of
// seconds or an HTTP date, as how long from now to wait. A header that is
// neither, or that is not in the future, says nothing: zero.
func retryAfter(header string, now time.Time) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(header, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(min(seconds, maxRetryAfter)) * time.Second
	}
	at, err := http.ParseTime(header)
	if err != nil || !at.After(now) {
		return 0
	}
	return at.Sub(now)
}
