package util

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"runtime/debug"
	"time"

	log "github.com/Sirupsen/logrus"
)

// JSONResponse represents an HTTP response which contains a JSON body.
type JSONResponse struct {
	// HTTP status code.
	Code int
	// JSON represents the JSON that should be serialized and sent to the client
	JSON interface{}
	// Headers represent any headers that should be sent to the client
	Headers map[string]string
}

// RedirectResponse returns a JSONResponse which 302s the client to the given location.
func RedirectResponse(location string) JSONResponse {
	headers := make(map[string]string)
	headers["Location"] = location
	return JSONResponse{
		Code:    302,
		JSON:    struct{}{},
		Headers: headers,
	}
}

// MessageResponse returns a JSONResponse with a 'message' key containing the given text.
func MessageResponse(code int, msg string) JSONResponse {
	return JSONResponse{
		Code: code,
		JSON: struct {
			Message string `json:"message"`
		}{msg},
	}
}

// ErrorResponse returns an HTTP 500 JSONResponse with the stringified form of the given error.
func ErrorResponse(err error) JSONResponse {
	return MessageResponse(500, err.Error())
}

// JSONRequestHandler represents an interface that must be satisfied in order to respond to incoming
// HTTP requests with JSON.
type JSONRequestHandler interface {
	OnIncomingRequest(req *http.Request) JSONResponse
}

// Protect panicking HTTP requests from taking down the entire process, and log them using
// the correct logger, returning a 500 with a JSON response rather than abruptly closing the
// connection. The http.Request MUST have a ctxValueLogger.
func Protect(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		defer func() {
			if r := recover(); r != nil {
				logger := req.Context().Value(ctxValueLogger).(*log.Entry)
				logger.WithFields(log.Fields{
					"panic": r,
				}).Errorf(
					"Request panicked!\n%s", debug.Stack(),
				)
				respond(w, req, MessageResponse(500, "Internal Server Error"))
			}
		}()
		handler(w, req)
	}
}

// MakeJSONAPI creates an HTTP handler which always responds to incoming requests with JSON responses.
// Incoming http.Requests will have a logger (with a request ID/method/path logged) attached to the Context.
// This can be accessed via GetLogger(Context).
func MakeJSONAPI(handler JSONRequestHandler) http.HandlerFunc {
	return Protect(func(w http.ResponseWriter, req *http.Request) {
		reqID := RandomString(12)
		// Set a Logger and request ID on the context
		ctx := context.WithValue(req.Context(), ctxValueLogger, log.WithFields(log.Fields{
			"req.method": req.Method,
			"req.path":   req.URL.Path,
			"req.id":     reqID,
		}))
		ctx = context.WithValue(ctx, ctxValueRequestID, reqID)
		req = req.WithContext(ctx)

		logger := req.Context().Value(ctxValueLogger).(*log.Entry)
		logger.Print("Incoming request")

		res := handler.OnIncomingRequest(req)

		// Set common headers returned regardless of the outcome of the request
		w.Header().Set("Content-Type", "application/json")
		SetCORSHeaders(w)

		respond(w, req, res)
	})
}

func respond(w http.ResponseWriter, req *http.Request, res JSONResponse) {
	logger := req.Context().Value(ctxValueLogger).(*log.Entry)

	// Set custom headers
	if res.Headers != nil {
		for h, val := range res.Headers {
			w.Header().Set(h, val)
		}
	}

	// Marshal JSON response into raw bytes to send as the HTTP body
	resBytes, err := json.Marshal(res.JSON)
	if err != nil {
		logger.WithError(err).Error("Failed to marshal JSONResponse")
		// this should never fail to be marshalled so drop err to the floor
		res = MessageResponse(500, "Internal Server Error")
		resBytes, _ = json.Marshal(res.JSON)
	}

	// Set status code and write the body
	w.WriteHeader(res.Code)
	logger.WithField("code", res.Code).Infof("Responding (%d bytes)", len(resBytes))
	w.Write(resBytes)
}

// WithCORSOptions intercepts all OPTIONS requests and responds with CORS headers. The request handler
// is not invoked when this happens.
func WithCORSOptions(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "OPTIONS" {
			SetCORSHeaders(w)
			return
		}
		handler(w, req)
	}
}

// SetCORSHeaders sets unrestricted origin Access-Control headers on the response writer
func SetCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept")
}

const alphanumerics = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// RandomString generates a pseudo-random string of length n.
func RandomString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alphanumerics[rand.Int63()%int64(len(alphanumerics))]
	}
	return string(b)
}

func init() {
	rand.Seed(time.Now().UTC().UnixNano())
}
