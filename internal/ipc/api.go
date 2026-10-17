package ipc

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"time"
)

// API is the protocol version ipc.capabilities reports. API 1 is the
// unversioned format before capabilities existed. The integer changes only for
// an incompatible change to the envelope or to the connection roles; new
// methods and topics are additive and are discovered through capabilities.
const API = 2

// Error codes. The vocabulary is closed: a reply carries one of these or no
// code at all. Legacy handlers' own errors carry no code.
const (
	CodeUnsupported   = "unsupported"
	CodeInvalidParams = "invalid_params"
	CodeLimitExceeded = "limit_exceeded"
	CodeTimeout       = "timeout"
	CodeCancelled     = "cancelled"
	CodeBusy          = "busy"
)

// Protocol limits, advertised in capabilities. They are fixed facts of the
// transport, not preferences.
const (
	lineBytes          = 64 << 10
	maxConsumers       = 32
	maxTopicsPerConn   = 16
	pendingBytes       = 2 << 20
	writeTimeoutMillis = int(writeTimeout / time.Millisecond)
)

// Error is a failure with a code from the closed vocabulary. An API-2 method
// returns one to set the reply's code; any other error is sent without a code.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// APIMethod answers one API-2 method. Its result is sent under "result", so it
// never collides with the envelope's own keys. A nil entry in Handlers.API is
// absent: it is not listed and it answers unsupported.
type APIMethod func(params json.RawMessage) (result any, err error)

// DecodeParams decodes API-2 params strictly: unknown fields, trailing data and
// type mismatches are invalid_params. Absent params decode as an empty object.
func DecodeParams(raw json.RawMessage, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &Error{Code: CodeInvalidParams, Message: "invalid params: " + err.Error()}
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return &Error{Code: CodeInvalidParams, Message: "invalid params: trailing data"}
	}
	return nil
}

// builtinAPI names the API-2 methods the transport itself owns. ipc.subscribe
// and ipc.unsubscribe change the connection, so the session answers them.
var builtinAPI = []string{"ipc.capabilities", "ipc.subscribe", "ipc.unsubscribe"}

type limits struct {
	LineBytes           int `json:"line_bytes"`
	Consumers           int `json:"consumers"`
	TopicsPerConnection int `json:"topics_per_connection"`
	PendingBytes        int `json:"pending_bytes"`
	WriteTimeoutMillis  int `json:"write_timeout_ms"`
}

type capabilities struct {
	API      int      `json:"api"`
	Methods  []string `json:"methods"`
	Families []string `json:"families"`
	Topics   []string `json:"topics"`
	Limits   limits   `json:"limits"`
}

func (s *Server) capabilities(raw json.RawMessage) (any, error) {
	var p struct{}
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	methods := Methods()
	for name, fn := range s.h.API {
		if fn != nil && !s.exactMethod(name) {
			methods = append(methods, name)
		}
	}
	sort.Strings(methods)
	topics := []string{}
	for name := range s.hubs {
		topics = append(topics, name)
	}
	sort.Strings(topics)
	return capabilities{
		API:      API,
		Methods:  methods,
		Families: MethodPrefixes(),
		Topics:   topics,
		Limits: limits{
			LineBytes:           lineBytes,
			Consumers:           maxConsumers,
			TopicsPerConnection: maxTopicsPerConn,
			PendingBytes:        pendingBytes,
			WriteTimeoutMillis:  writeTimeoutMillis,
		},
	}, nil
}

// exactMethod reports whether a name is a legacy or built-in method, which
// always wins over a Handlers.API entry of the same name.
func (s *Server) exactMethod(name string) bool {
	if _, ok := methods[name]; ok {
		return true
	}
	for _, b := range builtinAPI {
		if b == name {
			return true
		}
	}
	return false
}

// callAPI runs an API-2 method and wraps its outcome in the API-2 envelope.
func callAPI(id json.RawMessage, fn APIMethod, params json.RawMessage) []byte {
	result, err := fn(params)
	if err != nil {
		var coded *Error
		if errors.As(err, &coded) {
			return codedError(id, coded.Code, coded.Message)
		}
		return envelope(id, "", err.Error())
	}
	return resultEnvelope(id, result)
}

// codedError is an error reply that carries a code beside the message.
func codedError(id json.RawMessage, code, msg string) []byte {
	return marshalReply(struct {
		ID    json.RawMessage `json:"id"`
		Error string          `json:"error"`
		Code  string          `json:"code"`
	}{replyID(id), msg, code})
}

// resultEnvelope is an API-2 success: the body sits under "result".
func resultEnvelope(id json.RawMessage, result any) []byte {
	if result == nil {
		result = struct{}{}
	}
	return marshalReply(struct {
		ID     json.RawMessage `json:"id"`
		OK     bool            `json:"ok"`
		Result any             `json:"result"`
	}{replyID(id), true, result})
}

func replyID(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}

func marshalReply(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return codedError(nil, CodeInvalidParams, "unencodable result: "+err.Error())
	}
	return b
}
