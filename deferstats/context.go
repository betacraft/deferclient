// Based on Joe Shaw article https://joeshaw.org/net-context-and-http-handler/

package deferstats

import (
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ContextTracer passes spanId/parentSpanId parameters using context
type ContextTracer struct {
	SpanId       int64
	ParentSpanId int64
}

func (t *ContextTracer) newId() int64 {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return r.Int63()
}

// ResponseWriterExt implements http.ResponseWriter with extended methods
type ResponseWriterExt struct {
	w      http.ResponseWriter
	status int
	size   int
}

// Header is implementaion of standard http ResponseWriter Header method
func (e *ResponseWriterExt) Header() http.Header {
	return e.w.Header()
}

// Write is implementaion of standard http ResponseWriter Header method and setting the size and status
func (e *ResponseWriterExt) Write(b []byte) (int, error) {
	if e.status == 0 {
		// The status will be StatusOK if WriteHeader has not been
		// called yet
		e.status = http.StatusOK
	}
	size, err := e.w.Write(b)
	e.size += size
	return size, err
}

// WriteHeader is implementaion of standard http ResponseWriter WriteHeader method and setting the status
func (e *ResponseWriterExt) WriteHeader(s int) {
	e.w.WriteHeader(s)
	e.status = s
}

// Status returns the HTTP status code
func (e *ResponseWriterExt) Status() int {
	return e.status
}

// Size returns the HTTP size
func (e *ResponseWriterExt) Size() int {
	return e.size
}

// ContextBeforeRequest is called before request processing in context handler
func (c *Client) ContextBeforeRequest(w http.ResponseWriter, r *http.Request) (
	startTime time.Time, ext *ResponseWriterExt, tracer *ContextTracer, headers map[string]string) {
	startTime = time.Now()

	ext = &ResponseWriterExt{
		w: w,
	}

	tracer = new(ContextTracer)
	tracer.SpanId = tracer.newId()

	// add headers
	headers = make(map[string]string, len(r.Header))
	for k, v := range r.Header {
		headers[k] = strings.Join(v, ",")

		// grab SOA tracing header if present
		if k == "X-Dpparentspanid" {
			tracer.ParentSpanId, _ = strconv.ParseInt(v[0], 10, 64)
		}
	}

	return startTime, ext, tracer, headers
}

// ContextAfterRequest is called after request processing in context handler
func (c *Client) ContextAfterRequest(startTime time.Time, tracer *ContextTracer, r *http.Request,
	headers map[string]string, status_code int, isproblem bool) {
	appendHTTP(startTime, r.Method + " " + boneMux.GetRequestRoute(r), r.Method, status_code, tracer.SpanId,
		tracer.ParentSpanId, isproblem, headers)
}

// GetStatsURL returns statistics submitting URL
func (c *Client) GetStatsURL() (statsurl string) {
	return c.statsUrl
}

// SetStatsURL sets statistics submitting URL
func (c *Client) SetStatsURL(statsurl string) {
	c.statsUrl = statsurl
}

// ResetHTTPStats clears the current list of HTTP statistics
func ResetHTTPStats() {
	curlist.Reset()
}

// GetHTTPStats returns the current list of HTTP statistics
func GetHTTPStats() (deferhttps []DeferHTTP) {
	return curlist.List()
}
