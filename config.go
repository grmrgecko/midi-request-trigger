package main

import (
	"log"
	"os"
	"os/user"
	"path"
	"path/filepath"

	"github.com/kkyr/fig"
)

// Configurations relating to HTTP server.
type HTTPConfig struct {
	BindAddr string `fig:"bind_addr"`
	Port     uint   `fig:"port"`
	Debug    bool   `fig:"debug"`
	APIKey   string `fig:"api_key"`
	Enabled  bool   `fig:"enabled"`
}

// Configuration Structure.
type Config struct {
	HTTP        HTTPConfig    `fig:"http"`
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
		log.Fatal("Unable to find a configuration file.")
	}

	// Load the configuration file.
	config := &Config{
		HTTP: HTTPConfig{
			BindAddr: "",
			Port:     34936,
			Debug:    true,
			Enabled:  false,
		},
	}

	// Load configuration.
	filePath, fileName := path.Split(configFile)
	err = fig.Load(config,
		fig.File(fileName),
		fig.Dirs(filePath),
	)
	if err != nil {
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

	// Set global config structure.
	app.config = config
}
