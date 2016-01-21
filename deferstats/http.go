package deferstats

import (
	"fmt"
	"github.com/go-zoo/bone"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// curlist holds an array of DeferHTTPs (uri && latency)
	curlist = &deferHTTPList{}
	boneMux *bone.Mux
)

// HTTPPercentile is a single instance of the set of a http query percentiles
type HTTPPercentile struct {
	Sample DeferHTTP `json:"Sample"`
	P50    float64   `json:"P50"`
	P75    float64   `json:"P75"`
	P90    float64   `json:"P90"`
	P95    float64   `json:"P95"`
	P99    float64   `json:"P99"`
	Min    int64     `json:"Min"`
	Max    int64     `json:"Max"`
	Mean   float64   `json:"Mean"`
	StdDev float64   `json:"StdDev"`
	Count  int64     `json:"Count"`
}

type DeferHTTPs []DeferHTTP

func (slice DeferHTTPs) Len() int {
	return len(slice)
}

func (slice DeferHTTPs) Less(i, j int) bool {
	return slice[i].Time < slice[j].Time
}

func (slice DeferHTTPs) Swap(i, j int) {
	slice[i], slice[j] = slice[j], slice[i]
}

// getHTTPPercentiles returns a list of HTTPPercentiles from DeferHTTP
func getHTTPPercentiles(https []DeferHTTP) []HTTPPercentile {
	// build a map of paths to a list of latencies
	list := make(map[string]DeferHTTPs)

	for i := 0; i < len(https); i++ {
		p := https[i].Path

		if _, ok := list[p]; ok {
			list[p] = append(list[p], https[i])
		} else {
			s := DeferHTTPs{https[i]}

			list[p] = s
		}
	}

	var percentiles []HTTPPercentile

	// sort the list
	for _, v := range list {
		// sum
		sum := 0
		for i := 0; i < len(v); i++ {
			sum += v[i].Time
		}

		l := v.Len()
		fl := float64(l)

		// sort
		sort.Sort(v)

		p := HTTPPercentile{}

		e := (fl * float64(0.50))
		p.P50 = float64(v[int(e)].Time)

		e = (fl * float64(0.75))
		p.P75 = float64(v[int(e)].Time)

		e = (fl * float64(0.90))
		p.P90 = float64(v[int(e)].Time)

		e = (fl * float64(0.95))
		p.P95 = float64(v[int(e)].Time)

		e = (fl * float64(0.99))
		p.P99 = float64(v[int(e)].Time)

		p.Min = int64(v[0].Time)

		p.Max = int64(v[l-1].Time)

		p.Sample = v[l-1]

		p.Mean = float64(sum / l)

		p.Count = int64(l)

		p.StdDev = stdDev(v, p.Mean)

		percentiles = append(percentiles, p)

	}

	return percentiles
}

// stdDev is the square root of variance
func stdDev(vals DeferHTTPs, mean float64) float64 {
	// square root of variance
	return math.Sqrt(variance(vals, mean))
}

func variance(vals DeferHTTPs, mean float64) float64 {
	var tsum float64 = 0
	for i := 0; i < len(vals); i++ {
		b := (float64(vals[i].Time) - mean)
		tsum += (b * b)
	}
	return tsum / float64(len(vals))
}

// DeferHTTP holds a single instance of a http query
type DeferHTTP struct {
	Path         string            `json:"Path"`
	Method       string            `json:"Method"`
	StatusCode   int               `json:"StatusCode"`
	Time         int               `json:"Time"`
	SpanId       int64             `json:"SpanId"`
	ParentSpanId int64             `json:"ParentSpanId"`
	IsProblem    bool              `json:"IsProblem"`
	Headers      map[string]string `json:"Headers"`
}

// deferHTTPList is used to keep a list of DeferHTTP objects
// and interact with them in a thread-safe manner
type deferHTTPList struct {
	lock sync.RWMutex
	list []DeferHTTP
}

// tracingResponseWriter implements a responsewriter with status
// exportable
type tracingResponseWriter interface {
	http.ResponseWriter
	Status() int
	Size() int
}

// ResponseTracer implements a responsewriter with SpanId/ParentSpanId
type ResponseTracer struct {
	w            http.ResponseWriter
	status       int
	size         int
	SpanId       int64
	ParentSpanId int64
}

// Add adds a DeferHTTP object to the list
func (d *deferHTTPList) Add(item DeferHTTP) {
	d.lock.Lock()
	d.list = append(d.list, item)
	d.lock.Unlock()
}

// List returns a copy of the list
func (d *deferHTTPList) List() []DeferHTTP {
	d.lock.RLock()
	list := make([]DeferHTTP, len(d.list))
	for i, v := range d.list {
		list[i] = v
	}
	d.lock.RUnlock()
	return list
}

// Reset removes all entries from the list
func (d *deferHTTPList) Reset() {
	d.lock.Lock()
	d.list = []DeferHTTP{}
	d.lock.Unlock()
}

// WritePanicResponse is an overridable function that, by default, writes the contents of the panic
// error message with a 500 Internal Server Error.
var WritePanicResponse = func(w http.ResponseWriter, r *http.Request, errMsg string) {
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte(errMsg))
}

// appendHTTP adds a new http request to the list
func appendHTTP(startTime time.Time, path string, method string, status_code int, span_id int64,
	parent_span_id int64, isProblem bool, headers map[string]string) {
	endTime := time.Now()

	t := int(((endTime.Sub(startTime)).Nanoseconds() / 1000000))

	rpms.Inc(status_code)

	dh := DeferHTTP{
		Path:         path,
		Method:       method,
		Time:         t,
		StatusCode:   status_code,
		SpanId:       span_id,
		ParentSpanId: parent_span_id,
		IsProblem:    isProblem,
		Headers:      headers,
	}

	curlist.Add(dh)

}

// GetSpanIdString is a convenience method to get the string equivalent
// of a span id
func GetSpanIdString(r http.ResponseWriter) string {
	return strconv.FormatInt(GetSpanId(r), 10)
}

// GetSpanId returns the span id for this http request
func GetSpanId(r http.ResponseWriter) int64 {
	mPtr := (r).(*ResponseTracer)
	return mPtr.SpanId
}

func (l *ResponseTracer) newId() int64 {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return r.Int63()
}

// Header is implementaion of standard http ResponseWriter Header method
func (l *ResponseTracer) Header() http.Header {
	return l.w.Header()
}

// Write is implementaion of standard http ResponseWriter Header method and setting the size and status
func (l *ResponseTracer) Write(b []byte) (int, error) {
	if l.status == 0 {
		// The status will be StatusOK if WriteHeader has not been
		// called yet
		l.status = http.StatusOK
	}
	size, err := l.w.Write(b)
	l.size += size
	return size, err
}

// WriteHeader sets the header
func (l *ResponseTracer) WriteHeader(s int) {
	l.w.WriteHeader(s)
	l.status = s
}

// Status returns the HTTP status code
func (l *ResponseTracer) Status() int {
	return l.status
}

// Size returns the HTTP size
func (l *ResponseTracer) Size() int {
	return l.size
}

// HTTPHandlerFunc wraps a http handler func and captures the latency of each
// request
// this currently happens in a global list :( - TBFS
func (c *Client) HTTPHandlerFunc(f http.HandlerFunc) http.HandlerFunc {
	return c.HTTPHandler(f).(http.HandlerFunc)
}

// HTTPHandler wraps a http handler and captures the latency of each
// request
// this currently happens in a global list :( - TBFS
func (c *Client) HTTPHandler(f http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime, tracer, headers := c.BeforeRequest(w, r)

		defer func() {
			if err := recover(); err != nil {
				c.BaseClient.Prep(err, tracer.SpanId)
				c.AfterRequest(startTime, tracer, r, headers, 500, true)

				errorMsg := fmt.Sprintf("%v", err)
				WritePanicResponse(w, r, errorMsg)
			}
		}()

		f.ServeHTTP(tracer, r)

		c.AfterRequest(startTime, tracer, r, headers, tracer.Status(), false)
	})
}

// BeforeRequest is called before request processing in handler
func (c *Client) BeforeRequest(w http.ResponseWriter, r *http.Request) (
	startTime time.Time, tracer *ResponseTracer, headers map[string]string) {
	startTime = time.Now()

	tracer = &ResponseTracer{
		w: w,
	}
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

	return startTime, tracer, headers
}

// AfterRequest is called after request processing in handler
func (c *Client) AfterRequest(startTime time.Time, tracer *ResponseTracer, r *http.Request,
	headers map[string]string, status_code int, isproblem bool) {
	appendHTTP(startTime, r.Method + " "+ boneMux.GetRequestRoute(r), r.Method, status_code, tracer.SpanId,
		tracer.ParentSpanId, isproblem, headers)
}
