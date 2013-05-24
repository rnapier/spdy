package spdy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
)

// Response represents the response from a SPDY/HTTP request.
type Response struct {
	Status     string // e.g. "200 OK"
	StatusCode int    // e.g. 200
	Proto      string // e.g. "HTTP/1.0"
	ProtoMajor int    // e.g. 1
	ProtoMinor int    // e.g. 0
	SPDYProto  int    // SPDY version. Where SPDY was not used, this will be -1.

	// SentOverSpdy indicates whether the request was served over SPDY.
	SentOverSpdy bool

	// Header maps header keys to values.  If the response had multiple
	// headers with the same key, they will be concatenated, with comma
	// delimiters.  (Section 4.2 of RFC 2616 requires that multiple headers
	// be semantically equivalent to a comma-delimited sequence.) Values
	// duplicated by other fields in this struct (e.g., ContentLength) are
	// omitted from Header.
	//
	// Keys in the map are canonicalized (see CanonicalHeaderKey).
	Header http.Header

	// Body represents the response body.
	//
	// The http Client and Transport guarantee that Body is always
	// non-nil, even on responses without a body or responses with
	// a zero-lengthed body.
	//
	// The Body is automatically dechunked if the server replied
	// with a "chunked" Transfer-Encoding.
	Body io.ReadCloser

	// ContentLength records the length of the associated content.  The
	// value -1 indicates that the length is unknown.  Unless Request.Method
	// is "HEAD", values >= 0 indicate that the given number of bytes may
	// be read from Body.
	ContentLength int64

	// Contains transfer encodings from outer-most to inner-most. Value is
	// nil, means that "itentity" encoding is used. If SendOverSpdy is
	// true, then TransferEncoding will always be nil.
	TransferEncoding []string

	// Close records whether the header directed that the connection be
	// closed after reading Body. The value is advice for clients: neither
	// ReadResponse nor Response.Write ever closes a connection. If
	// SentOverSpdy is true, then Close will always be false.
	Close bool

	// Trailer maps trailer keys to values, in the same
	// format as the header.
	Trailer http.Header

	// The Request that was sent to obtain this Response.
	// Request's Body is nil (having already been consumed).
	// This is only populated for Client requests.
	Request *http.Request
}

// Cookies parses and returns the cookies set in the Set-Cookie headers.
func (r *Response) Cookies() []*http.Cookie {
	return spdyToHttpResponse(r, r.Request).Cookies()
}

// Location returns the URL of the response's "Location" header,
// if present.  Relative redirects are resolved relative to
// the Response's Request.  ErrNoLocation is returned if no
// Location header is present.
func (r *Response) Location() (*url.URL, error) {
	lv := r.Header.Get("Location")
	if lv == "" {
		return nil, http.ErrNoLocation
	}
	if r.Request != nil && r.Request.URL != nil {
		return r.Request.URL.Parse(lv)
	}
	return url.Parse(lv)
}

// ProtoAtLeast returns whether the HTTP protocol used
// in the response is at least major.minor.
func (r *Response) ProtoAtLeast(major, minor int) bool {
	return r.ProtoMajor > major ||
		r.ProtoMajor == major && r.ProtoMinor >= minor
}

type response struct {
	StatusCode int
	SPDYProto  int
	Header     http.Header
	Data       *bytes.Buffer
	Request    *http.Request
	Receiver   Receiver
}

func (r *response) ReceiveData(req *http.Request, data []byte, finished bool) {
	r.Data.Write(data)
	if r.Receiver != nil {
		r.Receiver.ReceiveData(req, data, finished)
	}
}

var statusRegex = regexp.MustCompile(`\A\s*(?P<code>\d+)`)

func (r *response) ReceiveHeaders(req *http.Request, headers http.Header) {
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	updateHeaders(r.Header, headers)
	if status := r.Header.Get(":status"); status != "" && statusRegex.MatchString(status) {
		if matches := statusRegex.FindAllStringSubmatch(status, -1); matches != nil {
			s, err := strconv.Atoi(matches[0][1])
			if err == nil {
				r.StatusCode = s
			}
		}
	}
	if r.Receiver != nil {
		r.Receiver.ReceiveHeaders(req, headers)
	}
}

func (r *response) ReceiveRequest(req *http.Request) bool {
	if r.Receiver != nil {
		return r.Receiver.ReceiveRequest(req)
	}
	return false
}

func (r *response) Response() *Response {
	if r.Data == nil {
		r.Data = new(bytes.Buffer)
	}
	out := new(Response)
	out.Status = fmt.Sprintf("%d %s", r.StatusCode, http.StatusText(r.StatusCode))
	out.StatusCode = r.StatusCode
	out.Proto = "HTTP/1.1"
	out.ProtoMajor = 1
	out.ProtoMinor = 1
	out.SPDYProto = r.SPDYProto
	out.SentOverSpdy = true
	out.Header = r.Header
	out.Body = &readCloserBuffer{r.Data}
	out.ContentLength = int64(r.Data.Len())
	out.TransferEncoding = nil
	out.Close = false
	out.Trailer = make(http.Header)
	out.Request = r.Request
	return out
}

type nilReceiver struct{}

func (_ nilReceiver) ReceiveData(_ *http.Request, _ []byte, _ bool) {
	return
}

func (_ nilReceiver) ReceiveHeaders(req *http.Request, headers http.Header) {
	return
}

func (_ nilReceiver) ReceiveRequest(req *http.Request) bool {
	return false
}

func spdyToHttpResponse(res *Response, req *http.Request) *http.Response {
	out := new(http.Response)
	out.Status = res.Status
	out.StatusCode = res.StatusCode
	out.Proto = res.Proto
	out.ProtoMajor = res.ProtoMajor
	out.ProtoMinor = res.ProtoMinor
	out.Header = res.Header
	out.Body = res.Body
	out.ContentLength = res.ContentLength
	out.TransferEncoding = res.TransferEncoding
	out.Close = res.Close
	out.Trailer = res.Trailer
	out.Request = req
	return out
}

func httpToSpdyResponse(res *http.Response, req *http.Request) *Response {
	out := new(Response)
	out.Status = res.Status
	out.StatusCode = res.StatusCode
	out.Proto = res.Proto
	out.ProtoMajor = res.ProtoMajor
	out.ProtoMinor = res.ProtoMinor
	out.SPDYProto = -1
	out.SentOverSpdy = false
	out.Header = res.Header
	out.Body = res.Body
	out.ContentLength = res.ContentLength
	out.TransferEncoding = res.TransferEncoding
	out.Close = res.Close
	out.Trailer = res.Trailer
	out.Request = req
	return out
}
