package websocket

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
)

const (
	finBit  = 1 << 7
	rsv1Bit = 1 << 6
	rsv2Bit = 1 << 5
	rsv3Bit = 1 << 4

	maskBit = 1 << 7

	maxHeaderSize     = 2 + 8 + 4
	maxPayloadDate    = 125
	defaultReaderSize = 1024
	defaultWriterSize = 1024

	noFrame = -1
)

const (
	ContMessage   = iota // 0
	TextMessage          // 1
	BinaryMessage        // 2
	_                    // 3
	_                    // 4
	_                    // 5
	_                    // 6
	_                    // 7
	CloseMessage         // 8
	PingMessage          // 9
	PongMessage          // 10
)

const (
	CloseNormalClosure           = 1000
	CloseGoingAway               = 1001
	CloseProtocolError           = 1002
	CloseUnsupportedData         = 1003
	CloseNoStatusReceived        = 1005
	CloseAbnormalClosure         = 1006
	CloseInvalidFramePayloadData = 1007
	ClosePolicyViolation         = 1008
	CloseMessageTooBig           = 1009
	CloseMandatoryExtension      = 1010
	CloseInternalServerErr       = 1011
	CloseServiceRestart          = 1012
	CloseTryAgainLater           = 1013
	CloseTLSHandshake            = 1015
)

var validReceivedCloseCodes = map[int]bool{
	CloseNormalClosure:           true,
	CloseGoingAway:               true,
	CloseProtocolError:           true,
	CloseUnsupportedData:         true,
	CloseNoStatusReceived:        false,
	CloseAbnormalClosure:         false,
	CloseInvalidFramePayloadData: true,
	ClosePolicyViolation:         true,
	CloseMessageTooBig:           true,
	CloseMandatoryExtension:      true,
	CloseInternalServerErr:       true,
	CloseServiceRestart:          true,
	CloseTryAgainLater:           true,
	CloseTLSHandshake:            false,
}

func isValidReceivedCloseCode(code int) bool {
	return validReceivedCloseCodes[code] || (code >= 3000 && code <= 4999)
}

type CloseError struct {
	Code int
	Text string
}

func (e *CloseError) Error() string {
	s := []byte("websocket: close ")
	s = strconv.AppendInt(s, int64(e.Code), 10)

	switch e.Code {
	case CloseNormalClosure:
		s = append(s, " (normal)"...)
	case CloseGoingAway:
		s = append(s, " (going away)"...)
	case CloseProtocolError:
		s = append(s, " (protocol error)"...)
	case CloseUnsupportedData:
		s = append(s, " (unsupported data)"...)
	case CloseNoStatusReceived:
		s = append(s, " (no status)"...)
	case CloseAbnormalClosure:
		s = append(s, " (abnormal closure)"...)
	case CloseInvalidFramePayloadData:
		s = append(s, " (invalid payload data)"...)
	case ClosePolicyViolation:
		s = append(s, " (policy violation)"...)
	case CloseMessageTooBig:
		s = append(s, " (message too big)"...)
	case CloseMandatoryExtension:
		s = append(s, " (manatory extension missing)"...)
	case CloseInternalServerErr:
		s = append(s, " (internal server error)"...)
	case CloseTLSHandshake:
		s = append(s, " (TLS handshake error)"...)
	}

	if e.Text != "" {
		s = append(s, ": "...)
		s = append(s, e.Text...)
	}

	return string(s)
}

var (
	errUnexpectedEOF       = &CloseError{Code: CloseAbnormalClosure, Text: io.ErrUnexpectedEOF.Error()}
	errInvalidControlFrame = errors.New("websocket: invalid control frame")
	errBadMessageCode      = errors.New("websocket: bad message code")
)

type Conn struct {
	conn net.Conn
	br   *bufio.Reader

	writer     io.WriteCloser
	writerSize int

	isServer bool
}

func newConn(conn net.Conn, isServer bool) *Conn {
	br := bufio.NewReaderSize(conn, defaultReaderSize)
	return &Conn{conn: conn, br: br, isServer: isServer}
}

func (c *Conn) Close() {
	c.conn.Close()
}

// write methods
type msgWriter struct {
	c   *Conn
	bw  *bufio.Writer
	fin bool
	mt  int
	ft  int
}

func (w *msgWriter) ncopy(max int) (int, error) {
	available := w.bw.Available() - maxHeaderSize
	if available <= 0 {
		if err := w.bw.Flush(); err != nil {
			return 0, err
		}

		available = w.bw.Available() - maxHeaderSize
	}

	if available < 126-8 {
		available += 8
	} else if available < 65536-6 {
		available += 6
	}

	if available >= max {
		w.fin = true
		available = max
	}

	return available, nil
}

func (w *msgWriter) writeFrame(payload []byte) error {
	b0 := byte(w.ft)
	if w.fin {
		b0 |= finBit
	}

	w.bw.WriteByte(b0)

	b1 := byte(0)
	if !w.c.isServer {
		b1 |= maskBit
	}
	length := len(payload)

	switch {
	case length >= 65536:
		b1 |= byte(127)
		w.bw.WriteByte(b1)
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(length))
		w.bw.Write(buf)

	case length > 125:
		b1 |= byte(126)
		w.bw.WriteByte(b1)
		buf := make([]byte, 2)
		binary.BigEndian.PutUint16(buf, uint16(length))
		w.bw.Write(buf)

	default:
		b1 |= byte(length)
		w.bw.WriteByte(b1)
	}

	if !w.c.isServer {
		mask, err := generateMask()
		if err != nil {
			return err
		}

		w.bw.Write(mask[:])

		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}

	w.bw.Write(payload)
	return nil
}

func (w *msgWriter) Write(p []byte) (int, error) {
	nn := len(p)
	w.ft = w.mt

	for len(p) >= 0 {
		n, err := w.ncopy(len(p))
		if err != nil {
			return 0, err
		}

		payload := p[:n]
		if err := w.writeFrame(payload); err != nil {
			return 0, err
		}

		p = p[n:]
		w.ft = ContMessage
		if len(p) == 0 {
			break
		}
	}

	w.fin = false
	return nn, nil
}

func (w *msgWriter) Close() error {
	writer := w
	w.c.writer = nil
	if err := writer.bw.Flush(); err != nil {
		return err
	}
	return nil
}

func (c *Conn) NextWriter(mt int) (io.WriteCloser, error) {
	if c.writer != nil {
		return nil, errors.New("unclosed previous writer")
	}

	bw := bufio.NewWriterSize(c.conn, maxHeaderSize+defaultWriterSize)
	writer := &msgWriter{c: c, bw: bw, mt: mt}

	c.writer = writer
	return c.writer, nil
}

func (c *Conn) WriteControl(mt int, msg []byte) error {
	if !isControl(mt) {
		return errBadMessageCode
	}

	var buf []byte
	b0 := finBit | byte(mt)
	b1 := byte(len(msg))
	if !c.isServer {
		b1 |= maskBit
	}

	buf = append(buf, b0, b1)
	if !c.isServer {
		mask, err := generateMask()
		if err != nil {
			return err
		}

		buf = append(buf, mask[:]...)

		for i := range msg {
			msg[i] ^= mask[i%4]
		}
	}

	buf = append(buf, msg...)

	if _, err := c.conn.Write(buf); err != nil {
		return err
	}

	return nil
}

func (c *Conn) WriteMessage(mt int, msg []byte) error {
	w, err := c.NextWriter(mt)
	if err != nil {
		return err
	}

	if _, err := w.Write(msg); err != nil {
		return err
	}
	return w.Close()
}

// read methods
func (c *Conn) read(n int) ([]byte, error) {
	p, err := c.br.Peek(n)
	if err == io.EOF {
		return nil, errUnexpectedEOF
	}

	c.br.Discard(len(p))
	return p, nil
}

func (c *Conn) ReadMessage() (int, []byte, error) {
	var msg []byte

	messageType := noFrame
	first := true

again:
	p, err := c.read(2)
	if err != nil {
		return noFrame, nil, err
	}

	frameType := int(p[0] & 0xf)
	final := p[0]&finBit != 0
	rsv1 := p[0]&rsv1Bit != 0
	rsv2 := p[0]&rsv2Bit != 0
	rsv3 := p[0]&rsv3Bit != 0

	mask := p[1]&maskBit != 0
	length := int64(p[1] & 0x7f)

	if mask != c.isServer {
		return noFrame, nil, errors.New("bad MASK")
	}

	var errors []string
	if rsv1 || rsv2 || rsv3 {
		errors = append(errors, "RSV bits set")
	}

	switch frameType {
	case CloseMessage, PingMessage, PongMessage:
		if length > maxPayloadDate {
			errors = append(errors, "len > 125 for control")
		}
		if !final {
			errors = append(errors, "FIN not set on control")
		}

	case TextMessage, BinaryMessage:
		messageType = frameType
		if !first {
			errors = append(errors, "data before FIN")
		}

	case ContMessage:
		if first {
			errors = append(errors, "continuation after FIN")
		}

	default:
		errors = append(errors, "bad opcode "+strconv.Itoa(frameType))
	}

	if len(errors) > 0 {
		return noFrame, nil, c.handleProtocolError(strings.Join(errors, ","))
	}

	switch length {
	case 126:
		p, err := c.read(2)
		if err != nil {
			return noFrame, nil, err
		}

		length = int64(binary.BigEndian.Uint16(p))

	case 127:
		p, err := c.read(8)
		if err != nil {
			return noFrame, nil, err
		}

		length = int64(binary.BigEndian.Uint64(p))
	}

	var maskKey [4]byte
	if mask {
		p, err := c.read(4)
		if err != nil {
			return noFrame, nil, err
		}

		copy(maskKey[:], p)
	}

	var buf []byte
	for length > 0 {
		p, err := c.read(int(length))
		if err != nil {
			return noFrame, nil, err
		}

		buf = append(buf, p...)
		length -= int64(len(p))
	}

	if mask {
		for i := range buf {
			buf[i] ^= maskKey[i%4]
		}
	}

	if isControl(frameType) {
		if err := c.handleControl(frameType, buf); err != nil {
			return noFrame, nil, err
		}
		if messageType != noFrame {
			first = false
		}
		goto again
	}

	msg = append(msg, buf...)

	if !final {
		first = false
		goto again
	}

	return messageType, msg, nil
}

func isControl(mt int) bool {
	return mt >= CloseMessage && mt <= PongMessage
}

func (c *Conn) handleControl(mt int, payload []byte) error {
	switch mt {
	case CloseMessage:
		code := CloseNoStatusReceived
		text := ""
		if len(payload) >= 2 {
			code = int(binary.BigEndian.Uint16(payload))
			if !isValidReceivedCloseCode(code) {
				return c.handleProtocolError("bad close code")
			}
			text = string(payload[2:])
		}
		c.handleClose(code, text)

		return &CloseError{Code: code, Text: text}

	case PingMessage:
		return c.handlePing(payload)
	case PongMessage:
		return c.handlePong()
	}

	return nil
}

func (c *Conn) handleClose(code int, text string) error {
	_ = c.WriteControl(CloseMessage, FormatCloseMessage(code, text))
	return nil
}

func (c *Conn) handlePing(payload []byte) error {
	_ = c.WriteControl(PongMessage, payload)
	return nil
}

func (c *Conn) handlePong() error {
	return nil
}

func (c *Conn) handleProtocolError(message string) error {
	data := FormatCloseMessage(CloseProtocolError, message)
	c.WriteMessage(CloseMessage, data)
	return errors.New("websocket: " + message)
}

func FormatCloseMessage(code int, msg string) []byte {
	if code == CloseNoStatusReceived {
		return []byte{}
	}

	buf := make([]byte, 2+len(msg))
	binary.BigEndian.PutUint16(buf, uint16(code))
	copy(buf[2:], msg)
	return buf
}
