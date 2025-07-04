package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	log "github.com/sirupsen/logrus"
	"gitlab.com/gomidi/midi/v2"
	"gitlab.com/gomidi/midi/v2/drivers"
)

// LogLevel Definition
type LogLevel int

const (
	// Logs info messages.
	InfoLog LogLevel = iota
	// Log only errors.
	ErrorLog
	// MQTT, HTTP, and MIDI receive logging.
	ReceiveLog
	// MQTT, HTTP, and MIDI send logging.
	SendLog
	// Debug messages.
	DebugLog
)

// Provides a string value for a log level.
func (l LogLevel) String() string {
	return [...]string{"Info", "Error", "Receive", "Send", "Debug"}[l]
}

// Configurations relating to MQTT connection.
type MQTTConfig struct {
	// Hostname of the MQTT broker.
	Host string `fig:"host"`
	// Port of the MQTT broker.
	Port int `fig:"port"`
	// MQTT client ID of this relay.
	ClientId string `fig:"client_id"`
	// User name used for MQTT authentication.
	User string `fig:"user"`
	// Password used for MQTT authentication.
	Password string `fig:"password"`
	// Topic where MQTT messages are pushed and received.
	// Set topic to `midi/example` and the following topics will be setup.
	// midi/example/cmd - Any commands received on MIDI will publish here.
	// midi/example/send - Any commands pushed via MQTT will be forwarded to MIDI.
	// midi/example/status - Configuration is published on startup.
	// midi/example/status/check - Request status.
	Topic string `fig:"topic"`
	// Disable sending all midi notes.
	DisableMidiFirehose bool `fig:"disable_midi_firehose"`
	// Disables the config send.
	DisableConfigSend bool `fig:"disable_config_send"`
}

// Payload to decode/encode JSON message.
type MQTTPayload struct {
	Channel  uint8 `json:"channel"`
	Note     uint8 `json:"note"`
	Velocity uint8 `json:"velocity"`
}

// Triggers that occur from MIDI messages received.
type NoteTrigger struct {
	// Channel to match.
	Channel uint8 `fig:"channel"`
	// If we should match all channel values.
	MatchAllChannels bool `fig:"match_all_channels"`
	// Note to match.
	Note uint8 `fig:"note"`
	// If we should match all note values.
	MatchAllNotes bool `fig:"match_all_notes"`
	// Velocity to match.
	Velocity uint8 `fig:"velocity"`
	// If we should match all velocity values.
	MatchAllVelocities bool `fig:"match_all_velocities"`
	// Allow delaying the request.
	DelayBefore time.Duration `fig:"delay_before"`
	DelayAfter  time.Duration `fig:"deplay_after"`
	// Custom MQTT message. Do not set to ignore MQTT.
	MqttTopic string `fig:"mqtt_topic"`
	// Nil payload will generate a payload with midi info.
	MqttPayload interface{} `fig:"mqtt_payload"`
	// If the HTTP request should includ midi info.
	MidiInfoInRequest bool `fig:"midi_info_in_request"`
	// Should SSL requests require a valid certificate.
	InsecureSkipVerify bool `fig:"insecure_skip_verify"`
	// The URL to call with the HTTP request. Do not set if you wish to not send HTTP request.
	URL string `fig:"url"`
	// HTTP method, defaults to GET.
	Method string `fig:"method"`
	// HTTP body.
	Body string `fig:"body"`
	// HTTP headers.
	Headers http.Header `fig:"headers"`
}

// Triggers that occur from HTTP or MQTT messsages received.
type RequestTrigger struct {
	Channel  uint8 `fig:"channel"`
	Note     uint8 `fig:"note"`
	Velocity uint8 `fig:"velocity"`
	// Parse midi notes from HTTP request.
	MidiInfoInRequest bool `fig:"midi_info_in_request"`
	// Absolute MQTT topic to subscribe.
	MqttTopic string `fig:"mqtt_topic"`
	// Sub topic off relay MQTT topic to subscribe.
	// midi/example/$SUB_TOPIC
	MqttSubTopic string `fig:"mqtt_sub_topic"`
	// Rather or not to disallow payload to be relayed.
	DisallowPayload bool `fig:"disallow_payload"`
	// Request URL path to trigger with.
	URI string `fig:"uri"`
}

// A common router for both receiving and sending MIDI messages.
type MidiRouter struct {
	// Used for human readable config.
	Name string `fig:"name"`
	// Midi device to connect, accepts regular expression.
	Device string `fig:"device"`
	// MQTT Connection if you are to integrate with MQTT.
	MQTT MQTTConfig `fig:"mqtt"`
	// Only connect for sending notes, not receiving.
	DisableListener bool `fig:"disable_listener"`
	// Listener triggers for notes to send HTTP and or MQTT messages.
	NoteTriggers []NoteTrigger `fig:"note_triggers"`
	// HTTP and or MQTT triggers to send MIDI notes.
	RequestTriggers []RequestTrigger `fig:"request_triggers"`

	// How much logging.
	// 0 - Info
	// 1 - Errors
	// 2 - MQTT, HTTP, and MIDI receive logging.
	// 3 - MQTT, HTTP, and MIDI send logging.
	// 4 - Debug
	LogLevel LogLevel `fig:"log_level"`

	// Connection to MIDI device.
	MidiOut drivers.Out `fig:"-"`
	// Function to stop listening to MIDI device.
	ListenerStop func() `fig:"-"`
	// The client connection to MQTT.
	MqttClient mqtt.Client `fig:"-"`
}

// Logging function to allow log levels.
func (r *MidiRouter) Log(level LogLevel, format string, args ...interface{}) {
	if level <= r.LogLevel {
		log.Println(fmt.Sprintf(format, args...))
	}
}

// When a MIDI message occurs, send the HTTP request.
func (r *MidiRouter) sendRequest(channel, note, velocity uint8) {
	// If MQTT firehose not disabled, send to general cmd topic.
	if r.MqttClient != nil && !r.MQTT.DisableMidiFirehose {
		payload := MQTTPayload{
			Channel:  channel,
			Note:     note,
			Velocity: velocity,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			r.Log(ErrorLog, "Json Encode: %s", err)
		} else {
			topic := r.MQTT.Topic + "/cmd"
			r.MqttClient.Publish(topic, 0, true, data)
			r.Log(SendLog, "-> [MQTT] %s: %s", topic, string(data))
		}
	}

	// Check each trigger to find requests that match this message.
	for _, trig := range r.NoteTriggers {
		// If match all notes, process this request.
		// If not, check if channel, note, and velocity matches.
		// The velocity may be defined to accept all.
		if (trig.Channel == channel || trig.MatchAllChannels) && (trig.Note == note || trig.MatchAllNotes) && (trig.Velocity == velocity || trig.MatchAllVelocities) {
			// For all logging, we want to print the message so setup a common string to print.
			logInfo := fmt.Sprintf("note %s(%d) on channel %v with velocity %v", midi.Note(note), note, channel, velocity)

			// Delay before.
			time.Sleep(trig.DelayBefore)

			// If MQTT trigger, send the MQTT request.
			if trig.MqttTopic != "" && r.MqttClient != nil {
				// If payload provided, send the defined payload.
				if trig.MqttPayload != nil {
					data, err := json.Marshal(trig.MqttPayload)
					if err != nil {
						r.Log(ErrorLog, "Json Encode: %s", err)
					} else {
						r.MqttClient.Publish(trig.MqttTopic, 0, true, data)
						r.Log(SendLog, "-> [MQTT] %s: %s", trig.MqttTopic, string(data))
					}
				} else {
					// If no payload provided, send the note information as JSON.
					payload := MQTTPayload{
						Channel:  channel,
						Note:     note,
						Velocity: velocity,
					}
					data, err := json.Marshal(payload)
					if err != nil {
						r.Log(ErrorLog, "Json Encode: %s", err)
					} else {
						r.MqttClient.Publish(trig.MqttTopic, 0, true, data)
						r.Log(SendLog, "-> [MQTT] %s: %s", trig.MqttTopic, string(data))
					}
				}
			}

			// If URL trigger defined, perform a HTTP request.
			if trig.URL != "" {
				// Default method to GET if nothing is defined.
				if trig.Method == "" {
					trig.Method = "GET"
				}

				// Parse the URL to make sure its valid.
				url, err := url.Parse(trig.URL)
				// If not valid, we need to stop processing this request.
				if err != nil {
					r.Log(ErrorLog, "Trigger failed to parse url: %s\n %s", err, logInfo)
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
				r.Log(DebugLog, "Starting request for trigger: %s %s\n%s", trig.Method, url.String(), logInfo)

				// Make the request.
				req, err := http.NewRequest(trig.Method, url.String(), body)
				if err != nil {
					r.Log(ErrorLog, "Trigger failed to parse url: %s\n %s", err, logInfo)
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
					r.Log(ErrorLog, "Trigger failed to request: %s\n %s", err, logInfo)
					continue
				}

				// Close the body at end of request.
				defer res.Body.Close()

				// If debug enabled, read the body and log it.
				if r.LogLevel >= DebugLog {
					body, err := io.ReadAll(res.Body)
					if err != nil {
						r.Log(ErrorLog, "Trigger failed to read body: %s\n %s", err, logInfo)
						continue
					}
					r.Log(DebugLog, "Trigger response: %s\n%s", logInfo, string(body))
				}
			}

			// Delay after.
			time.Sleep(trig.DelayAfter)
		}
	}
}

// Handler for HTTP requests.
func (m *MidiRouter) Handler(w http.ResponseWriter, r *http.Request) {
	// Check each request trigger for ones that match the request URI.
	for _, t := range m.RequestTriggers {
		// If matches request, process MIDI message.
		if t.URI != "" && t.URI == r.URL.RawPath {
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
				m.Log(ErrorLog, "Failed to get midi sender for request: %s\n%s", t.URI, err)
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
				m.Log(ErrorLog, "Failed to send midi message: %s\n%s", t.URI, err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}

			// Update HTTP status to no content as an success message.
			http.Error(w, http.StatusText(http.StatusNoContent), http.StatusNoContent)
		}
	}
}

// Send config to MQTT status.
func (r *MidiRouter) SendStatus() {
	// If disabled, ignore.
	if r.MQTT.DisableConfigSend {
		return
	}

	// Make JSON dump.
	config, err := json.Marshal(&r)
	if err != nil {
		r.Log(ErrorLog, "Json Error: %s", err)
	}

	// Send config.
	r.MqttClient.Publish(r.MQTT.Topic+"/status", 0, true, config)
}

// Handle MQTT events.
func (r *MidiRouter) MqttOnEvent(client mqtt.Client, message mqtt.Message) {
	r.Log(ReceiveLog, "<- [MQTT] %s: %s\n", message.Topic(), message.Payload())

	// Check commands to see if one matches this topic.
	for _, t := range r.RequestTriggers {
		if (t.MqttTopic != "" && message.Topic() == t.MqttTopic) ||
			(t.MqttSubTopic != "" && message.Topic() == r.MQTT.Topic+"/"+t.MqttSubTopic) {
			// Set default values to those from this trigger.
			channel, note, velocity := t.Channel, t.Note, t.Velocity

			// If arguments allowed and provided, parse, otherwise use default payload.
			arguments := MQTTPayload{
				Channel:  channel,
				Note:     note,
				Velocity: velocity,
			}
			if !t.DisallowPayload && len(message.Payload()) != 0 {
				err := json.Unmarshal(message.Payload(), &arguments)
				if err != nil {
					r.Log(ErrorLog, "Json Error: %s", err)
					return
				}
				channel = arguments.Channel
				note = arguments.Note
				velocity = arguments.Velocity
			}

			// Get send function for output.
			send, err := midi.SendTo(r.MidiOut)
			if err != nil {
				log.Printf("Failed to get midi sender for request: %s\n%s\n", message.Topic(), err)
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
				log.Printf("Failed to send midi message: %s\n%s\n", message.Topic(), err)
				return
			}
		}
	}

	// If standard send topic.
	if strings.HasPrefix(message.Topic(), r.MQTT.Topic+"/send") {
		// If arguments allowed and provided, parse, otherwise use default payload.
		var arguments MQTTPayload
		if len(message.Payload()) != 0 {
			err := json.Unmarshal(message.Payload(), &arguments)
			if err != nil {
				r.Log(ErrorLog, "Json Error: %s", err)
				return
			}
			// Get send function for output.
			send, err := midi.SendTo(r.MidiOut)
			if err != nil {
				log.Printf("Failed to get midi sender for request: %s\n%s\n", message.Topic(), err)
				return
			}

			// Make the MIDI message based on information.
			msg := midi.NoteOn(arguments.Channel, arguments.Note, arguments.Velocity)
			if arguments.Velocity == 0 {
				msg = midi.NoteOff(arguments.Channel, arguments.Note)
			}

			// Send MIDI message.
			err = send(msg)
			if err != nil {
				log.Printf("Failed to send midi message: %s\n%s\n", message.Topic(), err)
				return
			}
		}
	} else if message.Topic() == r.MQTT.Topic+"/status/check" {
		r.SendStatus()
	}
}

// Subscribe to MQTT Topic.
func (r *MidiRouter) MqttSubscribe(topic string) {
	r.Log(DebugLog, "Subscribing MQTT: %s", topic)
	if t := r.MqttClient.Subscribe(topic, 0, r.MqttOnEvent); t.Wait() && t.Error() != nil {
		r.Log(ErrorLog, "MQTT Subscribe Error: %s", t.Error())
	}
}

// Connect to MIDI devices and start listening.
func (r *MidiRouter) Connect() {
	// If request triggers defined, find the out port.
	if len(r.RequestTriggers) != 0 {
		go func() {
			deviceRx, err := regexp.Compile(r.Device)
			if err != nil {
				log.Printf("Failed to compile regexp of '%s': %v", r.Device, err)
			}
			for {
				var out drivers.Out
				for _, device := range midi.GetOutPorts() {
					if deviceRx.MatchString(device.String()) {
						err = device.Open()
						out = device
					}
				}
				if out == nil {
					err = fmt.Errorf("unable to find matching device")
				}
				if err != nil {
					r.Log(ErrorLog, "Failed to find output device '%s': %v", r.Device, err)
				} else {
					r.MidiOut = out
					break
				}

				r.Log(ErrorLog, "Retrying in 1 minute.")
				time.Sleep(time.Minute)
			}
		}()
	}

	// If listener is disabled, stop here.
	if !r.DisableListener {
		go func() {
			deviceRx, err := regexp.Compile(r.Device)
			if err != nil {
				log.Printf("Failed to compile regexp of '%s': %v", r.Device, err)
			}
			for {
				// Try finding input port.
				r.Log(InfoLog, "Connecting to input device: %s", r.Device)
				var in drivers.In
				for _, device := range midi.GetInPorts() {
					if deviceRx.MatchString(device.String()) {
						err = device.Open()
						in = device
					}
				}
				if in == nil {
					err = fmt.Errorf("unable to find matching device")
				}
				if err != nil {
					r.Log(ErrorLog, "Can't find input device '%s': %v", r.Device, err)
					r.Log(ErrorLog, "Retrying in 1 minute.")
					time.Sleep(time.Minute)
					continue
				}

				// Start listening to MIDI messages.
				stop, err := midi.ListenTo(in, func(msg midi.Message, timestampms int32) {
					var channel, note, velocity uint8
					switch {
					// Get notes with an velocity set.
					case msg.GetNoteStart(&channel, &note, &velocity):
						r.Log(ReceiveLog, "starting note %s(%d) on channel %v with velocity %v", midi.Note(note), note, channel, velocity)
						// Process request.
						r.sendRequest(channel, note, velocity)

						// If no velocity is set, an note end message is received.
					case msg.GetNoteEnd(&channel, &note):
						r.Log(ReceiveLog, "ending note %s(%d) on channel %v", midi.Note(note), note, channel)
						// Process request.
						r.sendRequest(channel, note, 0)
					default:
						// ignore
					}
				})
				if err != nil {
					r.Log(ErrorLog, "Error listening to device: %s", err)
					r.Log(ErrorLog, "Retrying in 1 minute.")
					time.Sleep(time.Minute)
					continue
				}
				r.Log(InfoLog, "Connected to input device: %s", r.Device)

				// Update stop function for disconnects.
				r.ListenerStop = stop
				break
			}
		}()
	}

	if r.MQTT.Host != "" && r.MQTT.Port != 0 {
		go func() {
			for {
				// Connect to MQTT.
				mqtt_opts := mqtt.NewClientOptions()
				mqtt_opts.AddBroker(fmt.Sprintf("tcp://%s:%d", r.MQTT.Host, r.MQTT.Port))
				mqtt_opts.SetClientID(r.MQTT.ClientId)
				mqtt_opts.SetUsername(r.MQTT.User)
				mqtt_opts.SetPassword(r.MQTT.Password)
				r.MqttClient = mqtt.NewClient(mqtt_opts)

				// Connect and failures are fatal exiting service.
				r.Log(DebugLog, "Connecting to MQTT")
				if t := r.MqttClient.Connect(); t.Wait() && t.Error() != nil {
					log.Fatalf("MQTT error: %s", t.Error())
					r.Log(ErrorLog, "Retrying in 1 minute.")
					time.Sleep(time.Minute)
					continue
				}

				// Subscribe to MQTT topics.
				r.MqttSubscribe(r.MQTT.Topic + "/send")
				r.MqttSubscribe(r.MQTT.Topic + "/status/check")
				// Subscribe to command topics configured.
				for _, trig := range r.RequestTriggers {
					if trig.MqttTopic != "" {
						r.MqttSubscribe(trig.MqttTopic)
					}
					if trig.MqttSubTopic != "" {
						r.MqttSubscribe(r.MQTT.Topic + "/" + trig.MqttSubTopic)
					}
				}
				break
			}
		}()
	}
}

// On disconnect, stop and remove output device.
func (r *MidiRouter) Disconnect() {
	r.MidiOut = nil
	if r.ListenerStop != nil {
		r.ListenerStop()
	}
	if r.MqttClient != nil {
		r.MqttClient.Disconnect(0)
	}
}
