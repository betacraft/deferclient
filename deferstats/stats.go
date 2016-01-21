// Package deferstats implements deferpanic stats
package deferstats

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"runtime"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/go-zoo/bone"

	"github.com/betacraft/deferclient/deferclient"
)

// being DEPRECATED
// please use deferstats.NewClient(token)
var (
	// Token is your deferpanic token available in settings
	Token string
)

// DeferStats captures {mem, gc, goroutines and http calls}
type DeferStats struct {
	Mem        string           `json:"Mem"`
	GC         string           `json:"GC"`
	LastGC     string           `json:"LastGC"`
	LastPause  string           `json:"LastPause"`
	GoRoutines string           `json:"GoRoutines"`
	Cgos       string           `json:"Cgos"`
	Fds        string           `json:"Fds"`
	Expvars    string           `json:"Expvars"`
	HTTPs      []HTTPPercentile `json:"HTTPs,omitempty"`
	DBs        []DeferDB        `json:"DBs,omitempty"`
	Rpms       Rpm              `json:"RPMs,omitempty"`
}

// Client is the client for making metrics requests to the
// defer panic api
type Client struct {
	// statsFrequency controls how often to report into deferpanic in seconds
	statsFrequency int

	// statsUrl is the stats api endpoint
	statsUrl string

	// GrabGC determines if we should grab gc stats
	GrabGC bool

	// GrabMem determines if we should grab mem stats
	GrabMem bool

	// GrabGR determines if we should grab go routine stats
	GrabGR bool

	// GrabCgo determines if we should grab cgo count
	GrabCgo bool

	// GrabFd determines if we should grab fd count
	GrabFd bool

	// GrabHTTP determines if we should grab http requests
	GrabHTTP bool

	// GrabExpvar determines if we should grab expvar
	GrabExpvar bool

	// LastGC keeps track of the last GC run
	LastGC int64

	// Token is your deferpanic token available in settings
	Token string

	// Verbose logs any stats output - for testing/dev
	Verbose bool

	// Environment sets an environment tag to differentiate between separate
	// environments - default is production.
	environment string

	// AppGroup sets an optional tag to differentiate between your various
	// services - default is default
	appGroup string

	// noPost when set to true disables reporting to deferpanic - useful
	// for dev/test envs
	noPost bool

	// GetExpvar captures expvar using Do method
	GetExpvar func() (string, error)

	// BaseClient is the base deferpanic client that all http requests use
	BaseClient *deferclient.DeferPanicClient
}

// NewClient instantiates and returns a new client
func NewClient(token string, mux *bone.Mux) *Client {

	ds := &Client{
		statsFrequency: 60,
		statsUrl:       deferclient.ApiBase + "/stats/create",
		GrabGC:         true,
		GrabMem:        true,
		GrabGR:         true,
		GrabCgo:        true,
		GrabFd:         true,
		GrabHTTP:       true,
		GrabExpvar:     false,
		Verbose:        false,
		Token:          token,
		environment:    "production",
		appGroup:       "default",
		noPost:         false,
	}

	ds.GetExpvar = func() (string, error) {
		return "", nil
	}

	ds.BaseClient = deferclient.NewDeferPanicClient(token)
	ds.BaseClient.Environment = ds.environment
	ds.BaseClient.AppGroup = ds.appGroup
	ds.BaseClient.NoPost = ds.noPost
	boneMux = mux

	return ds
}

// SetHttpProxy overrides the default httpclient
// with a proxy client with user given address
func (c *Client) SetHttpProxy(urlString string) error {
	proxyURL, err := url.Parse(urlString)
	if err != nil {
		return err
	}
	c.BaseClient.HttpClient = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	return nil
}

// Setenvironment sets the environment
// default is 'production'
func (c *Client) Setenvironment(environment string) {
	c.environment = environment
	c.BaseClient.Environment = c.environment
}

// SetappGroup sets the app group
// default is 'default'
func (c *Client) SetappGroup(appGroup string) {
	c.appGroup = appGroup
	c.BaseClient.AppGroup = c.appGroup
}

// Setnopost disables reporting to deferpanic
// default is false
func (c *Client) SetnoPost(noPost bool) {
	c.noPost = noPost
	c.BaseClient.NoPost = c.noPost
}

// CaptureStats POSTs DeferStats every statsFrequency
func (c *Client) CaptureStats() {
	defer func() {
		if rec := recover(); rec != nil {
			err := fmt.Sprintf("%q", rec)
			log.Println(err)
		}
	}()

	if !c.noPost {
		c.updateAgent()
	}

	tickerChannel := time.Tick(time.Duration(c.statsFrequency) * time.Second)
	for tc := range tickerChannel {

		// Capture the stats every statsFrequency seconds
		go c.capture()

		if c.Verbose {
			log.Printf("Captured at:%v\n", tc)
		}
	}
}

// updateAgent sets the agent details
func (c *Client) updateAgent() {
	if c.noPost {
		return
	}

	b, err := json.Marshal(c.BaseClient.Agent)
	if err != nil {
		log.Println(err)
	}

	agentUrl := deferclient.ApiBase + "/agent_ids/create"

	c.BaseClient.Postit(b, agentUrl, false)
}

// capture does a one time collection of DeferStats
func (c *Client) capture() {
	defer func() {
		if rec := recover(); rec != nil {
			err := fmt.Sprintf("%q", rec)
			log.Println(err)
		}
	}()

	var mem runtime.MemStats
	var gc debug.GCStats

	mems := ""
	if c.GrabMem {
		runtime.ReadMemStats(&mem)
		mems = strconv.FormatUint(mem.Alloc, 10)
	}

	gcs := ""
	var lastgc int64
	if c.GrabGC {
		debug.ReadGCStats(&gc)
		gcs = strconv.FormatInt(gc.NumGC, 10)
		lastgc = gc.LastGC.UnixNano()
	}

	grs := ""
	if c.GrabGR {
		grs = strconv.Itoa(runtime.NumGoroutine())
	}

	cgos := ""
	if c.GrabCgo {
		cgos = strconv.FormatInt(runtime.NumCgoCall(), 10)
	}

	fds := ""
	if c.GrabFd {
		fds = strconv.Itoa(openFileCnt())
	}

	ds := DeferStats{
		Mem:        mems,
		GoRoutines: grs,
		Cgos:       cgos,
		Fds:        fds,
		GC:         gcs,
		DBs:        Querylist.List(),
	}

	// reset dbs
	Querylist.Reset()

	if c.GrabHTTP {
		dhs := curlist.List()
		ds.HTTPs = getHTTPPercentiles(dhs)
		ds.Rpms = rpms.List()

		// reset http list && rpm
		curlist.Reset()
		rpms.ResetRPM()
	}

	if c.GrabExpvar {
		expvars, err := c.GetExpvar()
		if err != nil {
			log.Println(err)
		}
		ds.Expvars = expvars
	}

	if lastgc != c.LastGC {
		c.LastGC = lastgc
		ds.LastGC = strconv.FormatInt(c.LastGC, 10)
		ds.LastPause = strconv.FormatInt(gc.Pause[0].Nanoseconds(), 10)
	}

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				err := fmt.Sprintf("%q", rec)
				log.Println(err)
			}
		}()

		b, err := json.Marshal(ds)
		if err != nil {
			log.Println(err)
		}

		c.BaseClient.Postit(b, c.statsUrl, true)
	}()
}
