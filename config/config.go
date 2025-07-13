package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/teacat/chaturbate-dvr/entity"
	"github.com/urfave/cli/v2"
)

const configFilePath = "conf/settings.json"

// PersistentSettings holds the settings that should be saved between restarts
type PersistentSettings struct {
	Cookies           string `json:"cookies"`
	UserAgent         string `json:"user_agent"`
	Pattern           string `json:"pattern"`
	AutoDeleteWatched bool   `json:"auto_delete_watched"`
}

// New initializes a new Config struct with values from the CLI context.
func New(c *cli.Context) (*entity.Config, error) {
	config := &entity.Config{
		Version:           c.App.Version,
		Username:          c.String("username"),
		AdminUsername:     c.String("admin-username"),
		AdminPassword:     c.String("admin-password"),
		Framerate:         c.Int("framerate"),
		Resolution:        c.Int("resolution"),
		Pattern:           c.String("pattern"),
		MaxDuration:       c.Int("max-duration") * 60,
		MaxFilesize:       c.Int("max-filesize"),
		Port:              c.String("port"),
		Interval:          c.Int("interval"),
		Cookies:           c.String("cookies"),
		UserAgent:         c.String("user-agent"),
		Domain:            c.String("domain"),
		AutoDeleteWatched: false, // Default to false
	}

	// Load persistent settings if they exist
	if err := LoadPersistentSettings(config); err != nil {
		// If loading fails, just continue with CLI values
		// This is not a critical error
	}

	return config, nil
}

// LoadPersistentSettings loads saved settings from file
func LoadPersistentSettings(config *entity.Config) error {
	if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
		return nil // File doesn't exist, use defaults
	}

	data, err := os.ReadFile(configFilePath)
	if err != nil {
		return err
	}

	var settings PersistentSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return err
	}

	// Only override if CLI values are empty (not provided)
	if config.Cookies == "" && settings.Cookies != "" {
		config.Cookies = settings.Cookies
	}
	if config.UserAgent == "" && settings.UserAgent != "" {
		config.UserAgent = settings.UserAgent
	}
	if settings.Pattern != "" {
		config.Pattern = settings.Pattern
	}
	// Always load AutoDeleteWatched from settings
	config.AutoDeleteWatched = settings.AutoDeleteWatched

	return nil
}

// SavePersistentSettings saves current settings to file
func SavePersistentSettings(config *entity.Config) error {
	// Create conf directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(configFilePath), 0755); err != nil {
		return err
	}

	settings := PersistentSettings{
		Cookies:           config.Cookies,
		UserAgent:         config.UserAgent,
		Pattern:           config.Pattern,
		AutoDeleteWatched: config.AutoDeleteWatched,
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configFilePath, data, 0644)
}
