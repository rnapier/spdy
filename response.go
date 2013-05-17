package spdy

import (
	"io"
	"net/http"
)

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
	Header Header

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
	Trailer Header

	// The Request that was sent to obtain this Response.
	// Request's Body is nil (having already been consumed).
	// This is only populated for Client requests.
	Request *Request
}

func httpToSpdyResponse(res *http.Response, req *Request) *Response {
	out := new(Response)
	out.Status = res.Status
	out.StatusCode = res.StatusCode
	out.Proto = res.Proto
	out.ProtoMajor = res.ProtoMajor
	out.ProtoMinor = res.ProtoMinor
	out.SPDYProto = -1
	out.SentOverSpdy = false
	out.Header = Header(res.Header)
	out.Body = res.Body
	out.ContentLength = res.ContentLength
	out.TransferEncoding = res.TransferEncoding
	out.Close = res.Close
	out.Trailer = Header(res.Trailer)
	out.Request = req
	return out
}
