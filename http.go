package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
)

// Basic HTTP server structure.
type HTTPServer struct {
	server *http.Server
	mux    *mux.Router
	config *HTTPConfig
}

// This functions starts the HTTP server.
func NewHTTPServer() *HTTPServer {
	s := new(HTTPServer)
	// Update config reference.
	s.config = &app.config.HTTP
	s.server = &http.Server{}
	s.server.Addr = fmt.Sprintf("%s:%d", s.config.BindAddr, s.config.Port)

	// Setup router.
	r := mux.NewRouter()
	s.mux = r
	// Default to notice of service being online.
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "MIDI Request Trigger is available\n")
	})

	s.server.Handler = r
	// If the debug log is enabled, we'll add a middleware handler to log then pass the request to mux router.
	if app.config.HTTP.Debug {
		s.server.Handler = handlers.CombinedLoggingHandler(os.Stdout, r)
	}

	return s
}

// Start the HTTP server.
func (s *HTTPServer) Start(ctx context.Context) {
	isListening := make(chan bool)
	// Start server.
	go s.StartWithIsListening(ctx, isListening)
	// Allow the http server to initialize.
	<-isListening
}

// Starts the HTTP server with a listening channel.
func (s *HTTPServer) StartWithIsListening(ctx context.Context, isListening chan bool) {
	// Watch the background context for when we need to shutdown.
	go func() {
		<-ctx.Done()
		err := s.server.Shutdown(context.Background())
		if err != nil {
			// Error from closing listeners, or context timeout:
			log.Println("Error shutting down http server:", err)
		}
	}()

	// Start the server.
	log.Println("Starting http server:", s.server.Addr)
	l, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		log.Fatal("Listen: ", err)
	}
	// Now notify we are listening.
	isListening <- true
	// Serve http server on the listening port.
	err = s.server.Serve(l)
	if err != nil {
		log.Println("HTTP server failure:", err)
	}
}
