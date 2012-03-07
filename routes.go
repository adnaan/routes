package routes

import (
	"encoding/json"
	"encoding/xml"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	CONNECT = "CONNECT"
	DELETE  = "DELETE"
	GET     = "GET"
	HEAD    = "HEAD"
	OPTIONS = "OPTIONS"
	PATCH   = "PATCH"
	POST    = "POST"
	PUT     = "PUT"
	TRACE   = "TRACE"

	// log format, modeled after http://wiki.nginx.org/HttpLogModule
	LOG = `%s - - [%s] "%s %s %s" %d %d "%s" "%s"`

	// commonly used mime types
	applicationJson = "application/json"
	applicationXml  = "applicatoin/xml"
	textXml         = "text/xml"
)


var (
	DefaultAccessControlOrigin = "*"
)

type Route struct {
	method  string
	regex   *regexp.Regexp
	params  map[int]string
	handler http.HandlerFunc
}

type RouteMux struct {
	routes  []*Route
	Logging bool
	Logger  *log.Logger
}

func New() *RouteMux {
	routeMux := RouteMux{}
	routeMux.Logging = true
	routeMux.Logger = log.New(os.Stdout, "", 0)
	return &routeMux
}

// Adds a new Route to the Handler
func (this *RouteMux) AddRoute(method string, pattern string, handler http.HandlerFunc) *Route {

	//split the url into sections
	parts := strings.Split(pattern, "/")

	//find params that start with ":"
	//replace with regular expressions
	j := 0
	params := make(map[int]string)
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			expr := "([^/]+)"
			//a user may choose to override the defult expression
			// similar to expressjs: ‘/user/:id([0-9]+)’ 
			if index := strings.Index(part, "("); index != -1 {
				expr = part[index:]
				part = part[:index]
			}
			params[j] = part
			parts[i] = expr
			j++
		}
	}

	//recreate the pattern, with parameters replaced
	//by regular expressions. then compile the regex
	pattern = strings.Join(parts, "/")
	regex, regexErr := regexp.Compile(pattern)
	if regexErr != nil {
		//TODO add error handling here to avoid panic
		panic(regexErr)
		return nil
	}

	//now create the Route
	route := &Route{}
	route.method = method
	route.regex = regex
	route.handler = handler
	route.params = params

	//and finally append to the list of Routes
	this.routes = append(this.routes, route)

	return route
}

// Adds a new Route for GET requests
func (this *RouteMux) Get(pattern string, handler http.HandlerFunc) *Route {
	return this.AddRoute(GET, pattern, handler)
}

// Adds a new Route for PUT requests
func (this *RouteMux) Put(pattern string, handler http.HandlerFunc) *Route {
	return this.AddRoute(PUT, pattern, handler)
}

// Adds a new Route for DELETE requests
func (this *RouteMux) Del(pattern string, handler http.HandlerFunc) *Route {
	return this.AddRoute(DELETE, pattern, handler)
}

// Adds a new Route for PATCH requests
// See http://www.ietf.org/rfc/rfc5789.txt
func (this *RouteMux) Patch(pattern string, handler http.HandlerFunc) *Route {
	return this.AddRoute(PATCH, pattern, handler)
}

// Adds a new Route for POST requests
func (this *RouteMux) Post(pattern string, handler http.HandlerFunc) *Route {
	return this.AddRoute(POST, pattern, handler)
}

// Adds a new Route for Static http requests. Serves
// static files from the specified directory
func (this *RouteMux) Static(pattern string, dir string) *Route {
	//append a regex to the param to match everything
	// that comes after the prefix
	pattern = pattern + "(.+)"
	return this.AddRoute(GET, pattern, func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Clean(r.URL.Path)
		path = filepath.Join(dir, path)
		http.ServeFile(w, r, path)
	})
}

// Required by http.Handler interface. This method is invoked by the
// http server and will handle all page routing
func (this *RouteMux) ServeHTTP(rw http.ResponseWriter, r *http.Request) {

	requestPath := r.URL.Path

	//wrap the response writer, in our custom interface
	w := &responseWriter{writer: rw}

	//find a matching Route
	for _, Route := range this.routes {

		//if the methods don't match, skip this handler
		//i.e if request.Method is 'PUT' Route.Method must be 'PUT'
		if r.Method != Route.method { // && !(r.Method == HEAD && Route.method == GET) {
			continue
		}

		//check if Route pattern matches url
		if !Route.regex.MatchString(requestPath) {
			continue
		}

		//get submatches (params)
		matches := Route.regex.FindStringSubmatch(requestPath)

		//double check that the Route matches the URL pattern.
		if len(matches[0]) != len(requestPath) {
			continue
		}

		//add url parameters to the query param map
		values := r.URL.Query()
		for i, match := range matches[1:] {
			values.Add(Route.params[i], match)
		}

		//reassemble query params and add to RawQuery
		r.URL.RawQuery = url.Values(values).Encode()

		//Invoke the request handler
		Route.handler(w, r)

		break
	}

	//was this an OPTIONS request?
	if r.Method == OPTIONS {
		this.optionsRequest(w, r)
	}

	//if no matches to url, throw a not found exception
	if w.started == false {
		http.NotFound(w, r)
	}

	//if logging is turned on
	if this.Logging {
		this.Logger.Printf(LOG, r.RemoteAddr, time.Now().String(), r.Method,
			r.URL.Path, r.Proto, w.status, w.size,
			r.Referer(), r.UserAgent())
	}
}

// Performs an HTTP OPTIONS request on the request
// See http://ftp.ics.uci.edu/pub/ietf/http/draft-ietf-http-options-02.txt
// Example Response:
//    HTTP/1.1 200 OK
//    Date: Tue, 22 Jul 1997 20:21:51 GMT
//    Public: OPTIONS, GET, HEAD, PUT, POST, TRACE
//    Content-Length: 0
func (this *RouteMux) optionsRequest(w http.ResponseWriter, r *http.Request) {

	var options []string
	options = append(options, OPTIONS)
	requestPath := r.URL.Path

	//loop through all Routes and find those that match
	// the incoming request, regardless of type
	for _, Route := range this.routes {

		//check if Route pattern matches url
		if !Route.regex.MatchString(requestPath) {
			continue
		}

		//get submatches (params)
		matches := Route.regex.FindStringSubmatch(requestPath)

		//double check that the Route matches the URL pattern.
		if len(matches[0]) != len(requestPath) {
			continue
		}

		//append the options
		options = append(options, Route.method)
		if Route.method == GET {
			options = append(options, HEAD)
		}
	}

	sort.Strings(options)
	optionsStr := strings.Join(options, ", ")
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Public", optionsStr)
}

//responseWriter is a wrapper for the http.ResponseWriter
// to track if response was written to. It also allows us
// to automatically set certain headers, such as Content-Type,
// Access-Control-Allow-Origin, etc.
type responseWriter struct {
	writer  http.ResponseWriter // Writer
	started bool
	size    int
	status  int
}

// Header returns the header map that will be sent by WriteHeader.
func (this *responseWriter) Header() http.Header {
	return this.writer.Header()
}

// Write writes the data to the connection as part of an HTTP reply,
// and sets `started` to true
func (this *responseWriter) Write(p []byte) (int, error) {
	this.size = len(p)
	this.started = true
	this.writer.Header().Set("Content-Length", strconv.Itoa(this.size))
	this.writer.Header().Set("Access-Control-Allow-Origin", DefaultAccessControlOrigin)
	return this.writer.Write(p)
}

// WriteHeader sends an HTTP response header with status code,
// and sets `started` to true
func (this *responseWriter) WriteHeader(code int) {
	this.status = code
	this.started = true
	this.writer.WriteHeader(code)
	this.writer.Header().Set("Content-Length", "0")
	this.writer.Header().Set("Access-Control-Allow-Origin", DefaultAccessControlOrigin)
}


// ---------------------------------------------------------------------------------
// Below are helper functions to replace boilerplate
// code that serializes resources and writes to the
// http response.


// ServeJson replies to the request with a JSON
// representation of resource v.
func ServeJson(w http.ResponseWriter, v interface{}) {
	//content, err := json.Marshal(v)
	content, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(content)
	w.Header().Set("Content-Type", applicationJson)
}

// ServeXml replies to the request with an XML
// representation of resource v.
func ServeXml(w http.ResponseWriter, v interface{}) {
	content, err := xml.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(content)
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
}

// ServeFormatted replies to the request with
// a formatted representation of resource v, in the
// format requested by the client specified in the
// Accept header.
func ServeFormatted(w http.ResponseWriter, r *http.Request, v interface{}) {
	accept := r.Header.Get("Accept")
	switch accept {
	case applicationJson:
		ServeJson(w, v)
	case applicationXml, textXml:
		ServeXml(w, v)
	default:
		ServeJson(w, v)
	}

	return
}