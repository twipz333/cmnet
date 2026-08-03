// Package startup collects the messages produced before (and outside) any job, so
// the browser can show them.
//
// These used to go to stderr only. That is fine for the terminal front end and
// useless for the browser one: "background collection is off: another advisor
// holds the lock", "could not auto-install the GSI config", "public CM drafts are
// skipped without a STRATZ token" are exactly the messages that explain why the
// app appears to be doing nothing — and the user reading the browser UI never saw
// them.
//
// They still go to stderr as well. This is an addition, not a redirection: a user
// running from a console should not lose output because a second front end exists.
package startup

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Level classifies a startup message. Same three levels as the job log, so the
// browser filters them the same way.
const (
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

// Message is one line recorded before or outside a job.
type Message struct {
	At    int64  `json:"at"` // unix millis
	Level string `json:"level"`
	Text  string `json:"text"`
}

// maxMessages bounds the buffer. Startup is finite; this only guards against a
// caller putting something in a loop.
const maxMessages = 200

var (
	mu       sync.Mutex
	messages []Message
	// out is where messages are echoed. A variable so tests can capture it.
	out io.Writer = os.Stderr
)

// Infof records an informational message.
func Infof(format string, args ...any) { record(LevelInfo, fmt.Sprintf(format, args...)) }

// Warnf records something the user probably needs to know about but that does not
// stop the app.
func Warnf(format string, args ...any) { record(LevelWarn, fmt.Sprintf(format, args...)) }

// Errorf records a failure.
func Errorf(format string, args ...any) { record(LevelError, fmt.Sprintf(format, args...)) }

func record(level, text string) {
	mu.Lock()
	messages = append(messages, Message{At: time.Now().UnixMilli(), Level: level, Text: text})
	if len(messages) > maxMessages {
		messages = messages[len(messages)-maxMessages:]
	}
	w := out
	mu.Unlock()

	if w != nil {
		fmt.Fprintln(w, text)
	}
}

// Messages returns everything recorded so far, oldest first.
func Messages() []Message {
	mu.Lock()
	defer mu.Unlock()
	cp := make([]Message, len(messages))
	copy(cp, messages)
	return cp
}

// SetOutput replaces the echo destination; nil disables echoing. Returns the
// previous one so a caller can restore it.
func SetOutput(w io.Writer) io.Writer {
	mu.Lock()
	defer mu.Unlock()
	prev := out
	out = w
	return prev
}

// Reset clears the buffer. For tests.
func Reset() {
	mu.Lock()
	messages = nil
	mu.Unlock()
}
