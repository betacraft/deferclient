// Package deferclient implements access to the deferpanic api.
package deferclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
)

const (
	// ApiVersion is the version of this client
	ApiVersion = "v1.17"

	// ApiBase is the base url that client requests goto
	ApiBase = "https://api.deferpanic.com/" + ApiVersion

	// UserAgent is the User Agent that is used with this client
	UserAgent = "deferclient " + ApiVersion

	// errorsUrl is the url to post panics && errors to
	errorsUrl = ApiBase + "/panics/create"

	// cpuprofileUrl is the url to post cpuprofiles to
	cpuprofileUrl = ApiBase + "/uploads/cpuprofile/create"

	// memprofileUrl is the url to post memprofiles to
	memprofileUrl = ApiBase + "/uploads/memprofile/create"

	// traceUrl is the url to post traces to
	traceUrl = ApiBase + "/uploads/trace/create"
)

// being DEPRECATED
var (
	// Your deferpanic client token
	// this is being DEPRECATED
	Token string

	// Bool that turns off tracking of errors and panics - useful for
	// dev/test environments
	// this is being DEPRECATED
	NoPost = false
)

// DeferPanicClient is the base struct for making requests to the defer
// panic api
//
// FIXME: move all globals for future api bump
type DeferPanicClient struct {
	Token       string
	UserAgent   string
	Environment string
	AppGroup    string

	Agent       *Agent
	NoPost      bool
	PrintPanics bool

	HttpClient *http.Client

	RunningCommands map[int]bool
	sync.Mutex
}

// DeferJSON is a struct that holds json body for POSTing to deferpanic API
type DeferJSON struct {
	Msg       string `json:"ErrorName"`
	BackTrace string `json:"Body"`
	SpanId    int64  `json:"SpanId,omitempty"`
}

// Response is a struct that holds list of commands to be executed and agent state at server
type Response struct {
	Agent    Agent     `json:"AgentID"`
	Commands []Command `json:"Commands,omitempty"`
}

// NewDeferPanicClient instantiates and returns a new deferpanic client
func NewDeferPanicClient(token string) *DeferPanicClient {
	a := NewAgent()

	dc := &DeferPanicClient{
		Token:           token,
		UserAgent:       "deferclient " + ApiVersion,
		Agent:           a,
		PrintPanics:     false,
		NoPost:          false,
		RunningCommands: make(map[int]bool),
		HttpClient:      &http.Client{},
	}

	return dc
}

// Persist ensures any panics will post to deferpanic website for
// tracking
// typically used in non http go-routines
func (c *DeferPanicClient) Persist() {
	if err := recover(); err != nil {
		c.Prep(err, 0)
	}
}

// PersistRepanic ensures any panics will post to deferpanic website for
// tracking, it also reissues the panic afterwards.
// typically used in non http go-routines
func (c *DeferPanicClient) PersistRepanic() {
	if err := recover(); err != nil {
		c.PrepSync(err, 0)
		panic(err)
	}
}

// Prep takes an error && a spanId
// it cleans up the error/trace before calling ShipTrace
// if spanId is zero it is ommited
func (c *DeferPanicClient) Prep(err interface{}, spanId int64) {
	c.prep(err, spanId, false)
}

// PrepSync takes an error && a spanId
// it cleans up the error/trace before calling ShipTrace
// waits for ShipTrace, in a go routine, to complete before continuing
// if spanId is zero it is ommited
func (c *DeferPanicClient) PrepSync(err interface{}, spanId int64) {
	c.prep(err, spanId, true)
}

// prep is an internal function that can be called to synchronize after
// shipping the the trace to ensure completion.
func (c *DeferPanicClient) prep(err interface{}, spanId int64, syncShipTrace bool) {
	errorMsg := fmt.Sprintf("%q", err)

	errorMsg = strings.Replace(errorMsg, "\"", "", -1)

	if c.PrintPanics {
		stack := string(debug.Stack())
		fmt.Println(stack)
	}

	body := backTrace()

	if syncShipTrace {
		done := make(chan bool)
		go func() {
			c.ShipTrace(body, errorMsg, spanId)
			done <- true
		}()
		<-done
	} else {
		go c.ShipTrace(body, errorMsg, spanId)
	}
}

// cleanTrace should be fixed
// encoding
func cleanTrace(body string) string {
	body = strings.Replace(body, "\n", "\\n", -1)
	body = strings.Replace(body, "\t", "\\t", -1)
	body = strings.Replace(body, "\x00", " ", -1)
	body = strings.TrimSpace(body)

	return body
}

// ShipTrace POSTs a DeferJSON json body to the deferpanic website
// if spanId is zero it is ignored
func (c *DeferPanicClient) ShipTrace(exception string, errorstr string, spanId int64) {
	if c.NoPost {
		return
	}

	body := cleanTrace(exception)

	dj := &DeferJSON{
		Msg:       errorstr,
		BackTrace: body,
	}

	if spanId > 0 {
		dj.SpanId = spanId
	}

	b, err := json.Marshal(dj)
	if err != nil {
		log.Println(err)
	}

	c.Postit(b, errorsUrl, false)
}

// Postit Posts an API request w/b body to url and sets appropriate
// headers
func (c *DeferPanicClient) Postit(b []byte, url string, analyseResponse bool) {
	defer func() {
		if rec := recover(); rec != nil {
			err := fmt.Sprintf("%q", rec)
			log.Println(err)
		}
	}()

	if c.NoPost {
		return
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(b))

	req.Header.Set("X-deferid", c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("X-dpenv", c.Environment)
	req.Header.Set("X-dpgroup", c.AppGroup)
	req.Header.Set("X-dpagentid", c.Agent.Name)

	resp, err := c.HttpClient.Do(req)
	if err != nil {
		log.Println(err)
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 401:
		log.Println("wrong or invalid API token")
	case 429:
		log.Println("too many requests - you are being rate limited")
	case 503:
		log.Println("service not available")
	default:
	}

	if analyseResponse {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Println(err)
			return
		}

		var response Response
		err = json.Unmarshal(body, &response)
		if err != nil {
			log.Println(err)
			return
		}

		for _, command := range response.Commands {
			c.Lock()
			running := c.RunningCommands[command.Id]
			c.Unlock()
			if !running {
				switch command.Type {
				case CommandTypeTrace:
					go c.MakeTrace(command.Id, &response.Agent)
				case CommandTypeCPUProfile:
					go c.MakeCPUProfile(command.Id, &response.Agent)
				case CommandTypeMemProfile:
					go c.MakeMemProfile(command.Id, &response.Agent)
				default:
					log.Printf("Unknown command %v\n", command.Type)
				}
			}
		}
	}
}
