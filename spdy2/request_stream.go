// Copyright 2013 Jamie Hall. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package spdy2

import (
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/SlyMarbo/spdy/common"
	"github.com/SlyMarbo/spdy/spdy2/frames"
)

// RequestStream is a structure that implements
// the Stream and ResponseWriter interfaces. This
// is used for responding to client requests.
type RequestStream struct {
	sync.Mutex
	Request  *http.Request
	Receiver common.Receiver

	recvMutex    sync.Mutex
	shutdownOnce sync.Once
	conn         *Conn
	streamID     common.StreamID
	state        *common.StreamState
	output       chan<- common.Frame
	header       http.Header
	headerChan   chan func()
	responseCode int
	stop         <-chan bool
	finished     chan struct{}
}

func NewRequestStream(conn *Conn, streamID common.StreamID, output chan<- common.Frame) *RequestStream {
	out := new(RequestStream)
	out.conn = conn
	out.streamID = streamID
	out.output = output
	out.stop = conn.stop
	out.state = new(common.StreamState)
	out.state.CloseHere()
	out.header = make(http.Header)
	out.finished = make(chan struct{})
	out.headerChan = make(chan func(), 5)
	go out.processFrames()
	return out
}

/***********************
 * http.ResponseWriter *
 ***********************/

func (s *RequestStream) Header() http.Header {
	return s.header
}

// Write is one method with which request data is sent.
func (s *RequestStream) Write(inputData []byte) (int, error) {
	if s.closed() || s.state.ClosedHere() {
		return 0, errors.New("Error: Stream already closed.")
	}

	// Copy the data locally to avoid any pointer issues.
	data := make([]byte, len(inputData))
	copy(data, inputData)

	// Send any new headers.
	s.writeHeader()

	// Chunk the response if necessary.
	written := 0
	for len(data) > common.MAX_DATA_SIZE {
		dataFrame := new(frames.DATA)
		dataFrame.StreamID = s.streamID
		dataFrame.Data = data[:common.MAX_DATA_SIZE]
		s.output <- dataFrame

		written += common.MAX_DATA_SIZE
	}

	n := len(data)
	if n == 0 {
		return written, nil
	}

	dataFrame := new(frames.DATA)
	dataFrame.StreamID = s.streamID
	dataFrame.Data = data
	s.output <- dataFrame

	return written + n, nil
}

// WriteHeader is used to set the HTTP status code.
func (s *RequestStream) WriteHeader(int) {
	s.writeHeader()
}

/*****************
 * io.Closer *
 *****************/

// Close is used to cancel a mid-air
// request.
func (s *RequestStream) Close() error {
	defer common.Recover()
	s.Lock()
	s.shutdownOnce.Do(s.shutdown)
	s.Unlock()
	return nil
}

func (s *RequestStream) shutdown() {
	s.writeHeader()
	if s.state != nil {
		if s.state.OpenThere() {
			// Send the RST_STREAM.
			rst := new(frames.RST_STREAM)
			rst.StreamID = s.streamID
			rst.Status = common.RST_STREAM_CANCEL
			s.output <- rst
		}
		s.state.Close()
	}
	select {
	case <-s.finished:
	default:
		close(s.finished)
	}
	select {
	case <-s.headerChan:
	default:
		close(s.headerChan)
	}
	s.conn.requestStreamLimit.Close()
	s.output = nil
	s.Request = nil
	s.Receiver = nil
	s.header = nil
	s.stop = nil
}

/**********
 * Stream *
 **********/

func (s *RequestStream) Conn() common.Conn {
	return s.conn
}

func (s *RequestStream) ReceiveFrame(frame common.Frame) error {
	s.recvMutex.Lock()
	defer s.recvMutex.Unlock()

	if frame == nil {
		return errors.New("Nil frame received.")
	}

	// Process the frame depending on its type.
	switch frame := frame.(type) {
	case *frames.DATA:

		// Extract the data.
		data := frame.Data
		if data == nil {
			data = []byte{}
		}

		// Give to the client.
		s.headerChan <- func() {
			s.Receiver.ReceiveData(s.Request, data, frame.Flags.FIN())

			if frame.Flags.FIN() {
				s.state.CloseThere()
				s.Close()
			}
		}

	case *frames.SYN_REPLY:
		s.headerChan <- func() {
			s.Receiver.ReceiveHeader(s.Request, frame.Header)

			if frame.Flags.FIN() {
				s.state.CloseThere()
				s.Close()
			}
		}

	case *frames.HEADERS:
		s.headerChan <- func() {
			s.Receiver.ReceiveHeader(s.Request, frame.Header)

			if frame.Flags.FIN() {
				s.state.CloseThere()
				s.Close()
			}
		}

	case *frames.WINDOW_UPDATE:
		// Ignore.

	default:
		return errors.New(fmt.Sprintf("Received unknown frame of type %T.", frame))
	}

	return nil
}

func (s *RequestStream) CloseNotify() <-chan bool {
	return s.stop
}

// run is the main control path of
// the stream. Data is recieved,
// processed, and then the stream
// is cleaned up and closed.
func (s *RequestStream) Run() error {
	// Receive and process inbound frames.
	<-s.finished

	// Clean up state.
	s.state.CloseHere()
	return nil
}

func (s *RequestStream) State() *common.StreamState {
	return s.state
}

func (s *RequestStream) StreamID() common.StreamID {
	return s.streamID
}

func (s *RequestStream) closed() bool {
	if s.conn == nil || s.state == nil || s.Receiver == nil {
		return true
	}
	select {
	case _ = <-s.stop:
		return true
	default:
		return false
	}
}

// writeHeader is used to flush HTTP headers.
func (s *RequestStream) writeHeader() {
	if len(s.header) == 0 {
		return
	}

	// Create the HEADERS frame.
	header := new(frames.HEADERS)
	header.StreamID = s.streamID
	header.Header = common.CloneHeader(s.header)

	// Clear the headers that have been sent.
	for name := range header.Header {
		s.header.Del(name)
	}

	s.output <- header
}

func (s *RequestStream) processFrames() {
	defer common.Recover()
	for f := range s.headerChan {
		f()
	}
}
