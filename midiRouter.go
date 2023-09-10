package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"gitlab.com/gomidi/midi/v2"
	"gitlab.com/gomidi/midi/v2/drivers"
)

// Triggers that occur from MIDI messages received.
type NoteTrigger struct {
	MatchAllNotes      bool        `fig:"match_all_notes"`
	Channel            uint8       `fig:"channel"`
	Note               uint8       `fig:"note"`
	Velocity           uint8       `fig:"velocity"`
	MatchAllVelocities bool        `fig:"match_all_velocities"`
	MidiInfoInRequest  bool        `fig:"midi_info_in_request"`
	InsecureSkipVerify bool        `fig:"insecure_skip_verify"`
	URL                string      `fig:"url"`
	Method             string      `fig:"method"`
	Body               string      `fig:"body"`
	Headers            http.Header `fig:"headers"`
	DebugRequest       bool        `fig:"debug_request"`
}

// Triggers that occur from HTTP messsages received.
type RequestTrigger struct {
	Channel           uint8  `fig:"channel"`
	Note              uint8  `fig:"note"`
	Velocity          uint8  `fig:"velocity"`
	MidiInfoInRequest bool   `fig:"midi_info_in_request"`
	URI               string `fig:"uri"`
}

// A common router for both receiving and sending MIDI messages.
type MidiRouter struct {
	Name            string           `fig:"name"`
	Device          string           `fig:"device"`
	DebugListener   bool             `fig:"debug_listener"`
	DisableListener bool             `fig:"disable_listener"`
	NoteTriggers    []NoteTrigger    `fig:"note_triggers"`
	RequestTriggers []RequestTrigger `fig:"request_triggers"`

	MidiOut      drivers.Out `fig:"-"`
	ListenerStop func()      `fig:"-"`
}

// When a MIDI message occurs, send the HTTP request.
func (r *MidiRouter) sendRequest(channel, note, velocity uint8) {
	// Check each trigger to find requests that match this message.
	for _, trig := range r.NoteTriggers {
		// If match all notes, process this request.
		// If not, check if channel, note, and velocity matches.
		// The velocity may be defined to accept all.
		if trig.MatchAllNotes || (trig.Channel == channel && trig.Note == note && (trig.Velocity == velocity || trig.MatchAllVelocities)) {
			// For all logging, we want to print the message so setup a common string to print.
			logInfo := fmt.Sprintf("note %s(%d) on channel %v with velocity %v", midi.Note(note), note, channel, velocity)

			// Default method to GET if nothing is defined.
			if trig.Method == "" {
				trig.Method = "GET"
			}

			// Parse the URL to make sure its valid.
			url, err := url.Parse(trig.URL)
			// If not valid, we need to stop processing this request.
			if err != nil {
				log.Printf("Trigger failed to parse url: %s\n %s\n", err, logInfo)
				continue
			}

			// If MIDI info needs to be added to the request, add it.
			if trig.MidiInfoInRequest {
				query := url.Query()
				query.Add("channel", strconv.Itoa(int(channel)))
				query.Add("note", strconv.Itoa(int(note)))
				query.Add("velocity", strconv.Itoa(int(velocity)))
				url.RawQuery = query.Encode()
			}

			// If body provided, setup a reader for it.
			var body io.Reader
			if trig.Body != "" {
				body = strings.NewReader(trig.Body)
			}

			// If debugging, log that we're starting a request.
			if trig.DebugRequest {
				log.Printf("Starting request for trigger: %s %s\n%s\n", trig.Method, url.String(), logInfo)
			}

			// Make the request.
			req, err := http.NewRequest(trig.Method, url.String(), body)
			if err != nil {
				log.Printf("Trigger failed to parse url: %s\n %s\n", err, logInfo)
				continue
			}

			// Add headers to the request.
			req.Header = trig.Headers

			// Configure transport with trigger config.
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: trig.InsecureSkipVerify},
			}
			client := &http.Client{Transport: tr}

			// Perform the request.
			res, err := client.Do(req)
			if err != nil {
				log.Printf("Trigger failed to request: %s\n %s\n", err, logInfo)
				continue
			}

			// Close the body at end of request.
			defer res.Body.Close()

			// If debug enabled, read the body and log it.
			if trig.DebugRequest {
				body, err := io.ReadAll(res.Body)
				if err != nil {
					log.Printf("Trigger failed to read body: %s\n %s\n", err, logInfo)
					continue
				}
				log.Printf("Trigger response: %s\n%s\n", logInfo, string(body))
			}
		}
	}
}

// Handler for HTTP requests.
func (m *MidiRouter) Handler(w http.ResponseWriter, r *http.Request) {
	// Check each request trigger for ones that match the request URI.
	for _, t := range m.RequestTriggers {
		// If matches request, process MIDI message.
		if t.URI == r.URL.RawPath {
			// Set default values to those from this trigger.
			channel, note, velocity := t.Channel, t.Note, t.Velocity
			// If MIDI info is in the request query, update to request.
			if t.MidiInfoInRequest {
				query := r.URL.Query()
				// Regex to ensure only numbers are processed.
				numRx := regexp.MustCompile(`^[0-9]+$`)

				// Check for channel, and only configure if request has a valid value.
				ch := query.Get("channel")
				if numRx.MatchString(ch) {
					i, err := strconv.Atoi(ch)
					if err != nil && i <= 255 && i >= 0 {
						channel = uint8(i)
					}
				}
				// Check for note, and only configure if request has a valid value.
				key := query.Get("note")
				if numRx.MatchString(key) {
					i, err := strconv.Atoi(key)
					if err != nil && i < 255 && i >= 0 {
						note = uint8(i)
					}
				}
				// Check for velocity, and only configure if request has a valid value.
				vel := query.Get("velocity")
				if numRx.MatchString(vel) {
					i, err := strconv.Atoi(vel)
					if err != nil && i < 128 && i >= 0 {
						velocity = uint8(i)
					}
				}
			}

			// Get send function for output.
			send, err := midi.SendTo(m.MidiOut)
			if err != nil {
				log.Printf("Failed to get midi sender for request: %s\n%s\n", t.URI, err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}

			// Make the MIDI message based on information.
			msg := midi.NoteOn(channel, note, velocity)
			if velocity == 0 {
				msg = midi.NoteOff(channel, note)
			}

			// Send MIDI message.
			err = send(msg)
			if err != nil {
				log.Printf("Failed to send midi message: %s\n%s\n", t.URI, err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}

			// Update HTTP status to no content as an success message.
			http.Error(w, http.StatusText(http.StatusNoContent), http.StatusNoContent)
		}
	}
}

// Connect to MIDI devices and start listening.
func (r *MidiRouter) Connect() {
	// If request triggers defined, find the out port.
	if len(r.RequestTriggers) != 0 {
		out, err := midi.FindOutPort(r.Device)
		if err != nil {
			log.Println("Can't find output device:", r.Device)
		} else {
			r.MidiOut = out
		}
	}
	// If listener is disabled, stop here.
	if r.DisableListener {
		return
	}

	// Try finding input port.
	log.Println("Connecting to device:", r.Device)
	in, err := midi.FindInPort(r.Device)
	if err != nil {
		log.Println("Can't find device:", r.Device)
		return
	}

	// Start listening to MIDI messages.
	stop, err := midi.ListenTo(in, func(msg midi.Message, timestampms int32) {
		var channel, note, velocity uint8
		switch {
		// Get notes with an velocity set.
		case msg.GetNoteStart(&channel, &note, &velocity):
			// If debug, log.
			if r.DebugListener {
				log.Printf("starting note %s(%d) on channel %v with velocity %v\n", midi.Note(note), note, channel, velocity)
			}
			// Process request.
			r.sendRequest(channel, note, velocity)

		// If no velocity is set, an note end message is received.
		case msg.GetNoteEnd(&channel, &note):
			// If debug, log.
			if r.DebugListener {
				log.Printf("ending note %s(%d) on channel %v\n", midi.Note(note), note, channel)
			}
			// Process request.
			r.sendRequest(channel, note, 0)
		default:
			// ignore
		}
	})
	if err != nil {
		log.Printf("Error listening to device: %s\n", err)
		return
	}

	// Update stop function for disconnects.
	r.ListenerStop = stop
}

// On disconnect, stop and remove output device.
func (r *MidiRouter) Disconnect() {
	r.MidiOut = nil
	if r.ListenerStop != nil {
		r.ListenerStop()
	}
}
