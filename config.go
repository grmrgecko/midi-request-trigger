package main

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"runtime"

	"github.com/kkyr/fig"
	log "github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Configurations relating to HTTP server.
type HTTPConfig struct {
	BindAddr string `fig:"bind_addr"`
	Port     uint   `fig:"port"`
	Debug    bool   `fig:"debug"`
	APIKey   string `fig:"api_key"`
	Enabled  bool   `fig:"enabled"`
}

// Configuration for logging.
type LogConfig struct {
	// Limit the log output by the log level.
	Level string `fig:"level" yaml:"level" enum:"debug,info,warn,error" default:"info"`
	// How should the log output be formatted.
	Type string `fig:"type" yaml:"type" enum:"json,console" default:"console"`
	// The outputs that the log should go to. Output of `console` will
	// go to the stderr. An file path, will log to the file. Using `default-file`
	// it'll either save to `/var/log/name.log`, or to the same directory as the
	// executable if the path is not writable, or on Windows.
	Outputs []string `fig:"outputs" yaml:"outputs" default:"[console,default-file]"`
	// Maximum size of the log file in megabytes before it gets rotated.
	MaxSize int `fig:"max_size" yaml:"max_size" default:"1"`
	// Maximum number of backups to save.
	MaxBackups int `fig:"max_backups" yaml:"max_backups" default:"3"`
	// Maximum number of days to retain old log files.
	MaxAge int `fig:"max_age" yaml:"max_age" default:"0"`
	// Use the logal system time instead of UTC for file names of rotated backups.
	LocalTime *bool `fig:"local_time" yaml:"local_time" default:"true"`
	// Should the rotated logs be compressed.
	Compress *bool `fig:"compress" yaml:"compress" default:"true"`
}

// Apply log config.
func (l *LogConfig) Apply() {
	// Apply level.
	switch l.Level {
	case "debug":
		log.SetLevel(log.DebugLevel)
	case "info":
		log.SetLevel(log.InfoLevel)
	case "warn":
		log.SetLevel(log.WarnLevel)
	default:
		log.SetLevel(log.ErrorLevel)
	}

	// Apply type.
	switch l.Type {
	case "json":
		log.SetFormatter(&log.JSONFormatter{})
	default:
		log.SetFormatter(&log.TextFormatter{})
	}

	// Change the outputs.
	var outputs []io.Writer
	for _, output := range l.Outputs {
		// If output is console, add stderr and continue.
		if output == "console" {
			outputs = append(outputs, os.Stderr)
			continue
		}

		// If default-file defined, find the default file.
		if output == "default-file" {
			var f *os.File
			var err error
			var logDir, logPath string
			logName := fmt.Sprintf("%s.log", serviceName)

			// On *nix, `/var/log/` should be default if writable.
			if runtime.GOOS != "windows" {
				logDir = "/var/log"
				logPath = filepath.Join(logDir, logName)

				// Verify we can write to log file.
				f, err = os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
			}

			// If we could not open the file, then we should try the executable path.
			if err != nil || f == nil {
				exe, err := os.Executable()
				if err != nil {
					log.Println("Unable to find an writable log path to save log to.")
					continue
				} else {
					logDir = filepath.Dir(exe)
					logPath = filepath.Join(logDir, logName)

					// Verify we can write to log file.
					f, err = os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
					if err != nil {
						log.Println("Unable to find an writable log path to save log to.")
						continue
					} else {
						f.Close()
					}
				}
			} else {
				// Close file.
				f.Close()
			}

			// Update the config log path.
			output = logPath
		}

		// Setup lumberjack log rotate for the output, and add to the list.
		logFile := &lumberjack.Logger{
			Filename:   output,
			MaxSize:    l.MaxSize,
			MaxBackups: l.MaxBackups,
			MaxAge:     l.MaxAge,
			LocalTime:  *l.LocalTime,
			Compress:   *l.Compress,
		}
		outputs = append(outputs, logFile)
	}

	// If there are outputs, set the outputs.
	if len(outputs) != 0 {
		mw := io.MultiWriter(outputs...)
		log.SetOutput(mw)
	}
}

// Configuration Structure.
type Config struct {
	HTTP        HTTPConfig    `fig:"http"`
	Log         *LogConfig    `fig:"log" yaml:"log"`
	MidiRouters []*MidiRouter `fig:"midi_routers"`
}

// Load the configuration.
func (a *App) ReadConfig() {
	usr, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}

	// Configuration paths.
	localConfig, _ := filepath.Abs("./config.yaml")
	homeDirConfig := usr.HomeDir + "/.config/midi-request-trigger/config.yaml"
	etcConfig := "/etc/midi-request-trigger/config.yaml"

	// Determine which configuration to use.
	var configFile string
	if _, err := os.Stat(app.flags.ConfigPath); err == nil && app.flags.ConfigPath != "" {
		configFile = app.flags.ConfigPath
	} else if _, err := os.Stat(localConfig); err == nil {
		configFile = localConfig
	} else if _, err := os.Stat(homeDirConfig); err == nil {
		configFile = homeDirConfig
	} else if _, err := os.Stat(etcConfig); err == nil {
		configFile = etcConfig
	} else {
		log.Println("Unable to find a configuration file.")
	}

	// Load the configuration file.
	config := &Config{
		HTTP: HTTPConfig{
			BindAddr: "",
			Port:     34936,
			Debug:    true,
			Enabled:  false,
		},
		Log: &LogConfig{},
	}

	// Load configuration.
	filePath, fileName := path.Split(configFile)
	err = fig.Load(config,
		fig.File(fileName),
		fig.Dirs(filePath),
	)
	if err != nil {
		app.config = config
		log.Printf("Error parsing configuration: %s\n", err)
		return
	}

	// Flag Overrides.
	if app.flags.HTTPBind != "" {
		config.HTTP.BindAddr = app.flags.HTTPBind
	}
	if app.flags.HTTPPort != 0 {
		config.HTTP.Port = app.flags.HTTPPort
	}

	// Apply log configs.
	config.Log.Apply()

	// Set global config structure.
	app.config = config
}
