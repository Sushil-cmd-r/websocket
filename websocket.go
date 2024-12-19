package websocket

import (
	"errors"
	"net"
	"net/http"
	"net/url"
)

var (
	errBadUrl       = errors.New("bad ws or wss url")
	errBadHandshake = errors.New("bad handshake")
)

func Dial(wsUrl string) (*Conn, error) {
	u, err := url.Parse(wsUrl)
	if err != nil {
		return nil, errBadUrl
	}

	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, errBadUrl
	}

	challengeKey, err := generateChallengeKey()
	if err != nil {
		return nil, err
	}

	req := &http.Request{
		Method:     http.MethodGet,
		URL:        u,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
	}

	req.Header.Add("Upgrade", "websocket")
	req.Header.Add("Connection", "Upgrade")
	req.Header.Add("Sec-WebSocket-Version", "13")
	req.Header.Add("Sec-WebSocket-Key", challengeKey)

	netConn, err := net.Dial("tcp", u.Host)
	if err != nil {
		return nil, err
	}

	if err := req.Write(netConn); err != nil {
		netConn.Close()
		return nil, err
	}

	conn := newConn(netConn, false)
	_, err = http.ReadResponse(conn.br, req)
	if err != nil {
		conn.Close()
		return nil, err
	}

	// if !validateResponse(resp) ||
	// 	resp.Header.Get("Sec-WebSocket-Accept") != computeAcceptKey(challengeKey) {
	// 	conn.Close()
	// 	return nil, errBadHandshake
	// }

	return conn, nil
}

func validateResponse(resp *http.Response) bool {
	return resp.StatusCode == http.StatusSwitchingProtocols &&
		checkHeader(resp.Header, "Upgrade", "websocket") &&
		checkHeader(resp.Header, "Connection", "Upgrade")
}

func checkHeader(header http.Header, key, value string) bool {
	vals := header.Values(key)
	if len(vals) == 1 {
		return vals[0] == value
	}
	return false
}

func writeError(code int, msg string, w http.ResponseWriter) error {
	w.WriteHeader(code)
	w.Write([]byte(msg))
	return errors.New(msg)
}

func Accept(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	const badHandshake = "websocket: bad client handshake: "
	if !checkHeader(r.Header, "Connection", "Upgrade") {
		return nil, writeError(http.StatusBadRequest, badHandshake+"no 'Connection' header", w)
	}

	// if !checkHeader(r.Header, "Upgrade", "WebSocket") {
	// 	return nil, writeError(http.StatusBadRequest, badHandshake+"no 'Upgrade' header", w)
	// }

	if r.Method != http.MethodGet {
		return nil, writeError(http.StatusMethodNotAllowed, badHandshake+"not GET method", w)
	}

	if !checkHeader(r.Header, "Sec-WebSocket-Version", "13") {
		return nil, writeError(http.StatusBadRequest, badHandshake+"unsupported websocket version", w)
	}

	challengeKey := r.Header.Get("Sec-WebSocket-Key")
	if challengeKey == "" {
		return nil, writeError(http.StatusBadRequest, badHandshake+"no challenge key", w)
	}

	acceptKey := computeAcceptKey(challengeKey)

	w.Header().Set("Upgrade", "websocket")
	w.Header().Set("Connection", "Upgrade")
	w.Header().Set("Sec-WebSocket-Accept", acceptKey)
	w.WriteHeader(http.StatusSwitchingProtocols)

	h, ok := w.(http.Hijacker)
	if !ok {
		return nil, writeError(http.StatusInternalServerError, "websocket: response does not implement hijacker", w)
	}

	netConn, brw, err := h.Hijack()
	if err != nil {
		return nil, writeError(http.StatusInternalServerError, err.Error(), w)
	}

	if brw.Reader.Buffered() > 0 {
		netConn.Close()
		return nil, errors.New("websocket: client sent data with handshake")
	}

	conn := newConn(netConn, true)
	return conn, nil
}
