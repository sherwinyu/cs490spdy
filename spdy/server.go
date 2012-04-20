// spdy/server.go

package spdy

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"net/http"
	"io"
	"net"
	"os"
	"strconv"
	"time"
)

// ListenAndServe creates a new Server that serves on the given address.  If
// the handler is nil, then http.DefaultServeMux is used.
func ListenAndServe(addr string, handler http.Handler) os.Error {
	srv := &Server{addr, handler}
	return srv.ListenAndServe()
}

// ListenAndServeTLS acts like ListenAndServe except it uses TLS.
func ListenAndServeTLS(addr string, certFile, keyFile string, handler http.Handler) (err os.Error) {
	config := &tls.Config{
		Rand:         rand.Reader,
		Time:         time.Seconds,
		NextProtos:   []string{"http/1.1"},
		Certificates: make([]tls.Certificate, 1),
	}
	config.Certificates[0], err = tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return
	}

	conn, err := net.Listen("tcp", addr)
	if err != nil {
		return
	}
	tlsListener := tls.NewListener(conn, config)
	return (&Server{addr, handler}).Serve(tlsListener)
}

// A Server handles incoming SPDY connections with HTTP handlers.
type Server struct {
	Addr    string
	Handler http.Handler
}

// ListenAndServe services SPDY requests on the given address.
// If the handler is nil, then http.DefaultServeMux is used.
func (srv *Server) ListenAndServe() os.Error {
	addr := srv.Addr
	if addr == "" {
		addr = ":http"
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return srv.Serve(l)
}

// ListenAndServe services SPDY requests using the given listener.
// If the handler is nil, then http.DefaultServeMux is used.
func (srv *Server) Serve(l net.Listener) os.Error {
	defer l.Close()
	handler := srv.Handler
	if handler == nil {
		handler = http.DefaultServeMux
	}
	for {
		c, err := l.Accept()
		if err != nil {
			return err
		}
		s, err := newSession(c, handler)
		if err != nil {
			return err
		}
		go s.serve()
	}
	return nil
}

// A session manages a single TCP connection to a client.
type session struct {
	c       net.Conn
	handler http.Handler
	in, out chan Frame
	streams map[uint32]*serverStream // all access is done synchronously

	headerReader *HeaderReader
	headerWriter *HeaderWriter
}

func newSession(c net.Conn, h http.Handler) (s *session, err os.Error) {
	s = &session{
		c:            c,
		handler:      h,
		headerReader: NewHeaderReader(),
		headerWriter: NewHeaderWriter(-1),
		in:           make(chan Frame),
		out:          make(chan Frame),
		streams:      make(map[uint32]*serverStream),
	}
	return
}

func (sess *session) serve() {
	defer sess.c.Close()
	go sess.sendFrames()
	go sess.receiveFrames()

	for {
		select {
		case f := <-sess.in:
			switch frame := f.(type) {
			case ControlFrame:
				sess.handleControl(frame)
			case DataFrame:
				sess.handleData(frame)
			}
		}
	}
}

func (sess *session) handleControl(frame ControlFrame) {
	switch frame.Type {
	case TypeSynStream:
		if stream, err := newServerStream(sess, frame); err == nil {
			if _, exists := sess.streams[stream.id]; !exists {
				sess.streams[stream.id] = stream
				go func() {
					sess.handler.ServeHTTP(stream, stream.Request())
					stream.finish()
				}()
			}
		}
	case TypeRstStream:
		d := bytes.NewBuffer(frame.Data)
		var streamId, statusCode uint32
		readBinary(d, &streamId, &statusCode)
	case TypePing:
		d := bytes.NewBuffer(frame.Data)
		var pingId uint32
		readBinary(d, &pingId)
		sess.out <- ControlFrame{
			Type: TypePing,
			Data: []byte{
				byte(pingId & 0xff000000 >> 24),
				byte(pingId & 0x00ff0000 >> 16),
				byte(pingId & 0x0000ff00 >> 8),
				byte(pingId & 0x000000ff >> 0),
			},
		}
	}
}

func (sess *session) handleData(frame DataFrame) {
	st, found := sess.streams[frame.StreamID]
	if !found {
		// TODO: Error?
		return
	}
	if st.dataPipe != nil {
		st.dataPipe.write(frame.Data)
		if frame.Flags&FlagFin != 0 {
			st.dataPipe.wclose(nil)
		}
	}
}

func (sess *session) sendFrames() {
	for frame := range sess.out {
		// TODO: Check for errors
		frame.WriteTo(sess.c)
	}
}

func (sess *session) receiveFrames() {
	defer close(sess.in)
	for {
		frame, err := ReadFrame(sess.c)
		if err != nil {
			return
		}
		sess.in <- frame
	}
}

// A serverStream is a logical data stream inside a session.  A serverStream
// services a single request.
type serverStream struct {
	id      uint32
	session *session
	closed  bool

	requestHeaders  http.Header
	responseHeaders http.Header
	wroteHeader     bool

	dataPipe *asyncPipe
}

func newServerStream(sess *session, frame ControlFrame) (st *serverStream, err os.Error) {
	if frame.Type != TypeSynStream {
		err = os.NewError("Server stream must be created from a SynStream frame")
		return
	}
	st = &serverStream{
		session:         sess,
		responseHeaders: make(http.Header),
	}
	if frame.Flags&FlagFin == 0 {
		// Request body will follow
		st.dataPipe = apipe()
	}
	// Read frame data
	data := bytes.NewBuffer(frame.Data)
	err = binary.Read(data, binary.BigEndian, &st.id)
	if err != nil {
		return
	}
	_, err = io.ReadFull(data, make([]byte, 6)) // skip associated stream ID and priority
	if err != nil {
		return
	}
	st.requestHeaders, err = sess.headerReader.Decode(data.Bytes())
	return
}

// Request returns the request data associated with the serverStream.
func (st *serverStream) Request() (req *http.Request) {
	// TODO: Add more info
	req = &http.Request{
		Method:     st.requestHeaders.Get("method"),
		RawURL:     st.requestHeaders.Get("url"),
		Proto:      st.requestHeaders.Get("version"),
		Header:     st.requestHeaders,
		Body:       st,
		RemoteAddr: st.session.c.RemoteAddr().String(),
	}
	req.URL, _ = http.ParseRequestURL(req.RawURL)
	return
}

func (st *serverStream) Read(p []byte) (n int, err os.Error) {
	return st.dataPipe.read(p)
}

// Header returns the current response headers.
func (st *serverStream) Header() http.Header { return st.responseHeaders }

func (st *serverStream) Write(p []byte) (n int, err os.Error) {
	if st.closed {
		err = os.NewError("Write on closed serverStream")
		return
	}
	if !st.wroteHeader {
		st.WriteHeader(http.StatusOK)
	}
	for len(p) > 0 {
		frame := DataFrame{
			StreamID: st.id,
		}
		if len(p) < MaxDataLength {
			frame.Data = make([]byte, len(p))
		} else {
			frame.Data = make([]byte, MaxDataLength)
		}
		copy(frame.Data, p)
		p = p[len(frame.Data):]
		st.session.out <- frame
		n += len(frame.Data)
	}
	return
}

// A synReplyFrame defers header compression until the server writes the frame.
// This is necessary to guarantee correctly ordered compression.
type synReplyFrame struct {
	stream *serverStream
	header http.Header
	flags  FrameFlags
}

func (frame synReplyFrame) GetFlags() FrameFlags {
	return frame.flags
}

func (frame synReplyFrame) GetData() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, frame.stream.id&0x7fffffff)
	buf.Write([2]byte{}[:])
	frame.stream.session.headerWriter.WriteHeader(buf, frame.stream.responseHeaders)
	return buf.Bytes()
}

func (frame synReplyFrame) WriteTo(w io.Writer) (n int64, err os.Error) {
	cf := ControlFrame{Type: TypeSynReply, Data: frame.GetData()}
	return cf.WriteTo(w)
}

func (st *serverStream) WriteHeader(code int) {
	if st.wroteHeader {
		return
	}
	st.responseHeaders.Set("status", strconv.Itoa(code)+" "+http.StatusText(code))
	st.responseHeaders.Set("version", "HTTP/1.1")
	if st.responseHeaders.Get("Content-Type") == "" {
		st.responseHeaders.Set("Content-Type", "text/html; charset=utf-8")
	}
	if st.responseHeaders.Get("Date") == "" {
		st.responseHeaders.Set("Date", time.UTC().Format(http.TimeFormat))
	}
	// Write the frame
	// TODO: Copy headers
	st.session.out <- synReplyFrame{stream: st, header: st.responseHeaders}
	st.wroteHeader = true
}

// Close sends a closing frame, thus preventing the server from sending more
// data over the stream.  The client may still send data.
func (st *serverStream) Close() (err os.Error) {
	if st.closed {
		return
	}
	st.session.out <- DataFrame{
		StreamID: st.id,
		Flags:    FlagFin,
		Data:     []byte{},
	}
	st.closed = true
	return nil
}

func (st *serverStream) finish() (err os.Error) {
	if !st.wroteHeader {
		st.WriteHeader(http.StatusOK)
	}
	return st.Close()
}