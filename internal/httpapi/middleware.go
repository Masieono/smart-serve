package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// contentSecurityPolicy is the policy every response carries.
//
// Nothing in this application loads from anywhere but its own origin: the assets are
// embedded, there is no CDN, no web font, no analytics and no third-party script. A
// policy that says `'self'` and nothing else therefore costs nothing to enforce — and
// it closes the class of bug that matters here, where device-supplied strings (a model
// name, a user's note on a drive, a `smartctl` log line, a serial echoed into a URL)
// are rendered and must never be interpreted as markup or as a request somewhere else.
//
// `style-src` allows inline styles, and that is deliberate rather than lazy: the shell
// emits the theme as a `style` attribute before first paint, which is what prevents the
// flash of the wrong theme (`docs/UI.md` §5). There is no equivalent need for scripts —
// no inline handler and no inline script exists — so `script-src` gets no allowance.
// That makes the one directive that would actually let injected markup run the one
// directive that is strict.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// securityHeadersMiddleware attaches the headers that describe what a response is
// allowed to *do*, as opposed to what it says.
//
// It sits outside the CSRF guard so that a rejected request carries them too: a 403
// rendered by a browser is still a document, and there is no reason for it to be a
// less constrained one than a 200.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		// A browser that sniffs a JSON error body as HTML turns a message into a page.
		h.Set("X-Content-Type-Options", "nosniff")
		// Drive serials and mount points are in these paths, and this app is reached
		// from machines whose network a referer would leak them to.
		h.Set("Referrer-Policy", "no-referrer")
		// `frame-ancestors` above is the modern spelling; this is the one older
		// browsers honour, and being framed is never a thing this UI needs.
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// recordingWriter remembers whether the response has begun.
//
// It exists for the panic path: once a status line is on the wire the only honest
// thing left to do is stop, because a 500 appended to half a JSON document is a worse
// answer than a truncated one. It also keeps streaming working — `handleEvents` asks
// for `http.Flusher` by type assertion, so a wrapper that forgot `Flush` would not
// break visibly, it would turn every live scan into "streaming unsupported".
type recordingWriter struct {
	http.ResponseWriter
	status int
	bytes  int
	wrote  bool
}

func (w *recordingWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
	}
	w.wrote = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *recordingWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status = http.StatusOK
	}
	w.wrote = true
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func (w *recordingWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the writer underneath, so anything using
// the controller (a write deadline, for instance) is not blocked by the wrapper.
func (w *recordingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// recoverMiddleware turns a handler panic into a 500 the client can actually read.
//
// Without it a panic unwinds to net/http, which closes the connection and logs to
// stderr: the browser sees a network error with no body, the SPA shows nothing useful,
// and the only record is a stack trace in the container log with no request attached.
// Recovering here keeps the failure inside the protocol the client speaks, and attaches
// the method and path to the log line so the trace is findable.
//
// The message sent is deliberately generic. A panic value is an internal detail, and
// this handler is one of the few places where the temptation to echo one is strongest
// — the reader is already getting a 500, so the only thing a detail adds is a hint at
// what to probe.
func recoverMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &recordingWriter{ResponseWriter: w}
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			// ErrAbortHandler is not a failure: handlers panic with it to abort a
			// response on purpose, and net/http suppresses the stack for it. Logging
			// it as a crash would turn a deliberate abort into noise worth ignoring.
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			logger.Error("handler panic",
				"method", r.Method,
				"path", r.URL.Path,
				"panic", rec,
				"stack", string(debug.Stack()),
			)
			if rw.wrote {
				return
			}
			writeError(rw, http.StatusInternalServerError, "internal error")
		}()
		next.ServeHTTP(rw, r)
	})
}

// newRef is a short reference for one failed request.
//
// Four bytes, because the reference is read aloud off a screen and typed into a search
// box: long enough that two failures in the same log will not collide, short enough to
// survive being retyped. It is not a secret and does not need to be.
func newRef() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A reference that cannot be generated is not worth failing a request over: the
		// log line still has everything, and "unknown" is honest about what happened.
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}

// logMiddleware records one line per request: method, path, status, bytes and duration.
//
// Without it the only evidence a request ever happened is the absence of a complaint,
// which makes "are we 500ing", "what is slow" and "was that endpoint even called"
// unanswerable without a debugger. One line per request is cheap: an idle tab holds a
// single long-lived SSE stream and produces exactly one line, when that stream closes.
//
// It sits outermost so the status it records is the status the client actually got,
// including the 500 that recoverMiddleware synthesises from a panic.
func logMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rw := &recordingWriter{ResponseWriter: w}
		defer func() {
			status := rw.status
			if status == 0 {
				// No status line was written: the handler returned without answering, or a
				// panic re-raised as http.ErrAbortHandler unwound past recovery. net/http's
				// own answer — an empty 200 for a bare return, a closed connection for the
				// abort — is written after this runs and is not visible here; a 0 would be
				// skipped by a log scan, so the request is recorded as a failure.
				status = http.StatusInternalServerError
			}
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"bytes", rw.bytes,
				"duration_ms", time.Since(started).Milliseconds(),
			}
			switch {
			case status >= 500:
				logger.Error("request", attrs...)
			case status >= 400:
				// A 4xx is usually the client's mistake — an unknown serial, a bad scan
				// target — but it is the line that explains a broken bookmark, so it is a
				// warning rather than silence.
				logger.Warn("request", attrs...)
			default:
				logger.Info("request", attrs...)
			}
		}()
		next.ServeHTTP(rw, r)
	})
}
