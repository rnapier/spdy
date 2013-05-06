package spdy

import (
  "errors"
  "fmt"
)

type flowControl struct {
  stream         *stream
  active         bool
  initialWindow  uint32
  transferWindow int64
  sent           uint32
  buffer         [][]byte
  constrained    bool
}

func (s *stream) AddFlowControl() {
  flow := new(flowControl)
	initialWindow := s.conn.initialWindowSize
  if s.version == 3 {
    flow.active = true
    flow.buffer = make([][]byte, 0, 10)
    flow.initialWindow = initialWindow
		flow.transferWindow = int64(initialWindow)
    flow.stream = s
  }
  s.flow = flow
}

func (f *flowControl) CheckInitialWindow() {
  newWindow := f.stream.conn.initialWindowSize

  if f.initialWindow != newWindow {
    if f.initialWindow > newWindow {
      f.transferWindow = int64(newWindow - f.sent)
    } else if f.initialWindow < newWindow {
      f.transferWindow += int64(newWindow - f.initialWindow)
    }
    if f.transferWindow <= 0 {
      f.constrained = true
    }
    f.initialWindow = newWindow
  }
}

func (f *flowControl) UpdateWindow(deltaWindowSize uint32) error {
  if int64(deltaWindowSize)+f.transferWindow > MAX_TRANSFER_WINDOW_SIZE {
    return errors.New("Error: WINDOW_UPDATE delta window size overflows transfer window size.")
  }

  // Grow window and flush queue.
  fmt.Printf("Flow: Growing window in stream %d by %d bytes.\n", f.stream.streamID, deltaWindowSize)
  f.transferWindow += int64(deltaWindowSize)

  f.Flush()
  return nil
}

func (f *flowControl) Write(data []byte) (int, error) {
  l := len(data)
  if l == 0 {
    return 0, nil
  }

  // Transfer window processing.
  if f.active {
    f.CheckInitialWindow()
    if f.active && f.constrained {
      f.Flush()
    }
    var window uint32
    if f.transferWindow < 0 {
      window = 0
    } else {
      window = uint32(f.transferWindow)
    }

    if uint32(len(data)) > window {
      f.buffer = append(f.buffer, data[window:])
      data = data[:window]
      f.sent += window
      f.transferWindow -= int64(window)
      f.constrained = true
      fmt.Printf("Stream %d is now constrained.\n", f.stream.streamID)
    }
  }

  if len(data) == 0 {
    return l, nil
  }

  dataFrame := new(DataFrame)
  dataFrame.StreamID = f.stream.streamID
  dataFrame.Data = data

  f.stream.output <- dataFrame
  return l, nil
}

func (f *flowControl) Flush() {
  f.CheckInitialWindow()
  if !f.active || !f.constrained || f.transferWindow == 0 {
    return
  }

  out := make([]byte, 0, f.transferWindow)
  left := f.transferWindow
  for i := 0; i < len(f.buffer); i++ {
    if l := int64(len(f.buffer[i])); l <= left {
      out = append(out, f.buffer[i]...)
      left -= l
      f.buffer = f.buffer[1:]
    } else {
      out = append(out, f.buffer[i][:left]...)
      f.buffer[i] = f.buffer[i][left:]
      left = 0
    }

    if left == 0 {
      break
    }
  }

  f.transferWindow -= int64(len(out))

  if f.transferWindow > 0 {
    f.constrained = false
    fmt.Printf("Stream %d is no longer constrained.\n", f.stream.streamID)
  }

  dataFrame := new(DataFrame)
  dataFrame.StreamID = f.stream.streamID
  dataFrame.Data = out

  f.stream.output <- dataFrame
}

func (f *flowControl) Paused() bool {
  f.CheckInitialWindow()
  return f.active && f.constrained
}
