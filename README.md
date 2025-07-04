# midi-request-trigger

A service that triggers HTTP requests and/or MQTT messages when MIDI messages are recieved and triggers MIDI messages when HTTP requests and/or MQTT messages are received.

## Install

You can install by building.

### Building

Building should be as simple as running:

```bash
go build
```

### Running as a service

You are likely going to want to run the tool as a service to ensure it runs at boot and restarts in case of failures. Below is an example service config file you can place in `/etc/systemd/system/midi-request-trigger.service` on a linux system to run as a service if you install the binary in `/usr/local/bin/`.

```systemd
[Unit]
Description=MIDI Request Trigger
After=network.target
StartLimitIntervalSec=500
StartLimitBurst=5

[Service]
ExecStart=/usr/local/bin/midi-request-trigger
ExecReload=/bin/kill -s HUP $MAINPID
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

Once the service file is installed, you can run the following to start it:

```bash
systemctl daemon-reload
systemctl start midi-request-trigger.service
```

On MacOS, you can setup a Launch Agent in `~/Library/LaunchAgents/com.mrgeckosmedia.midi-request-trigger.plist` as follows:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.mrgeckosmedia.midi-request-trigger</string>
	<key>ProgramArguments</key>
	<array>
		<string>/path/to/bin/midi-request-trigger</string>
        <string>-c</string>
        <string>/path/to/config.yaml</string>
	</array>
	<key>KeepAlive</key>
	<dict>
		<key>Crashed</key>
		<true/>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>RunAtLoad</key>
	<true/>
    <key>OnDemand</key>
    <false/>
</dict>
</plist>
```

For local network connection, you need to code sign your build.
```bash
codesign -s - --force --deep /path/to/bin/midi-request-trigger
```

Start with:
```bash
launchctl load ~/Library/LaunchAgents/com.mrgeckosmedia.midi-request-trigger.plist
```

Check status with:
```bash
launchctl list com.mrgeckosmedia.midi-request-trigger
```

Stop with:
```bash
launchctl unload ~/Library/LaunchAgents/com.mrgeckosmedia.midi-request-trigger.plist
```


## Config

The default configuration paths are:

- `./config.yaml` - A file in the current working directory.
- `~/.config/midi-request-trigger/config.yaml` - A file in your home directory's config path.
- `/etc/midi-request-trigger/config.yaml` - A file in the etc config folder.

### To verify listener works

You can find the device name by running the following:
```bash
midi-request-trigger -l
```

On MacOS, there is an IAC Driver that can be enabled in Audio MIDI Setup.
```yaml
---
midi_routers:
  - name: service_notifications
    device: IAC Driver Bus 1
    log_level: 2
```

### Example note trigger configuration

```yaml
---
midi_routers:
  - name: service_notifications
    device: IAC Driver Bus 1
    log_level: 2
    note_triggers:
      - channel: 0
        note: 0
        match_all_velocities: true
        url: http://example.com
        midi_info_in_request: true
```

### Example request trigger configuration

```yaml
---
midi_routers:
  - name: service_notifications
    device: IAC Driver Bus 1
    log_level: 2
    request_triggers:
      - channel: 0
        note: 0
        velocity: 1
        midi_info_in_request: true
        uri: /send_note
```

### Example multi part request

```yaml
---
midi_routers:
  - name: service_notifications
    device: IAC Driver Bus 1
    log_level: 3
    note_triggers:
      - channel: 0
        note: 0
        match_all_velocities: true
        url: http://example.com
        method: POST
        body: |
          -----------------------------888832887744
          Content-Disposition: form-data; name="message"

          example variable
          -----------------------------888832887744
          Content-Disposition: form-data; name="file"; filename="example.txt"
          Content-Type: text/plain

          Content of file.

          -----------------------------888832887744--
        headers:
          Content-Type:
            - multipart/form-data; boundary=---------------------------888832887744
```

### Example mqtt config

```yaml
---
midi_routers:
    - name: Wing Midi Signals
      device: WING Port 4
      mqtt:
        host: 10.0.0.2
        port: 1883
        client_id: midi_mqtt_bridge
        user: mqtt
        password: password
        topic: midi/behringer_wing
      log_level: 4
      note_triggers:
        - channel: 0
          note: 1
          match_all_velocities: true
          mqtt_topic: osc/behringer_wing/send/$ctl/user/2/2/enc/val
          mqtt_payload:
            - "1"
        - channel: 0
          note: 2
          match_all_velocities: true
          mqtt_topic: osc/behringer_wing/send/$ctl/user/2/2/enc/val
          mqtt_payload:
            - "2"
        - channel: 0
          note: 3
          match_all_velocities: true
          mqtt_topic: osc/behringer_wing/send/$ctl/user/2/2/enc/val
          mqtt_payload:
            - "3"
        - channel: 0
          note: 4
          match_all_velocities: true
          mqtt_topic: osc/behringer_wing/send/$ctl/user/2/2/enc/val
          mqtt_payload:
            - "4"
        - channel: 0
          note: 5
          match_all_velocities: true
          mqtt_topic: osc/behringer_wing/send/$ctl/user/2/2/enc/val
          mqtt_payload:
            - "5"
        - channel: 0
          note: 6
          match_all_velocities: true
          mqtt_topic: osc/behringer_wing/send/$ctl/user/2/2/enc/val
          mqtt_payload:
            - "6"
        - channel: 0
          note: 7
          match_all_velocities: true
          mqtt_topic: osc/behringer_wing/send/$ctl/user/2/2/enc/val
          mqtt_payload:
            - "7"
        - channel: 0
          note: 8
          match_all_velocities: true
          mqtt_topic: osc/behringer_wing/send/$ctl/user/2/2/enc/val
          mqtt_payload:
            - "8"
        - channel: 0
          match_all_notes: true
          match_all_velocities: true
          delay_before: 200ms
          mqtt_topic: osc/behringer_wing/send/$ctl/user/2/2/bu/val
          mqtt_payload:
            - "1"
        - channel: 0
          match_all_notes: true
          match_all_velocities: true
          delay_before: 200ms
          mqtt_topic: osc/behringer_wing/send/$ctl/user/2/2/bu/val
          mqtt_payload:
            - "0"
          delay_after: 200ms
```
