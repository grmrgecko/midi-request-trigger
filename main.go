package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"gitlab.com/gomidi/midi/v2"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
)

const (
	serviceName        = "midi-request-trigger"
	serviceDescription = "Takes trigger MIDI messages by HTTP or MQTT requests and trigger HTTP or MQTT requests by MIDI messages"
	serviceVersion     = "0.2.2"
)

// App is the global application structure for communicating between servers and storing information.
type App struct {
	flags  *Flags
	config *Config
	http   *HTTPServer
}

var app *App

func main() {
	app = new(App)
	app.ParseFlags()
	app.ReadConfig()
	app.http = NewHTTPServer()

	// Make sure midi drivers are closed when the app closes.
	defer midi.CloseDriver()

	// If no routers defined, or request to list devices.
	if app.flags.ListMidiDevices || len(app.config.MidiRouters) == 0 {
		// If no routers are defined, print notice about configuring one.
		if len(app.config.MidiRouters) == 0 {
			log.Println("No routers configured, please configure one.")
		}
		// Print available devices.
		fmt.Printf("MIDI in ports\n")
		fmt.Println(midi.GetInPorts())
		fmt.Printf("\n\nMIDI out ports\n")
		fmt.Println(midi.GetOutPorts())
		fmt.Printf("\n\n")
		return
	}

	// Connect to each router and and setup HTTP handlers.
	for _, router := range app.config.MidiRouters {
		router.Connect()
		for _, trig := range router.RequestTriggers {
			app.http.mux.HandleFunc(trig.URI, router.Handler)
		}
	}

	// Setup context with cancellation function to allow background services to gracefully stop.
	ctx, ctxCancel := context.WithCancel(context.Background())
	// Start listening on HTTP server.
	app.http.Start(ctx)

	// Monitor common signals.
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	// Wait for a signal.
	<-c
	// Stop HTTP server.
	ctxCancel()

	// Disconnect all MIDI listeners.
	for _, router := range app.config.MidiRouters {
		router.Disconnect()
	}
}
