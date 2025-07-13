package entity

import (
	"regexp"
	"strings"
)

// Event represents the type of event for the channel.
type Event = string

const (
	EventUpdate Event = "update"
	EventLog    Event = "log"
)

// ChannelConfig represents the configuration for a channel.
type ChannelConfig struct {
	IsPaused    bool   `json:"is_paused"`
	Username    string `json:"username"`
	Framerate   int    `json:"framerate"`
	Resolution  int    `json:"resolution"`
	Pattern     string `json:"pattern"`
	MaxDuration int    `json:"max_duration"`
	MaxFilesize int    `json:"max_filesize"`
	CreatedAt   int64  `json:"created_at"`
}

func (c *ChannelConfig) Sanitize() {
	c.Username = regexp.MustCompile(`[^a-zA-Z0-9_-]`).ReplaceAllString(c.Username, "")
	c.Username = strings.TrimSpace(c.Username)
}

// ChannelInfo represents the information about a channel,
// mostly used for the template rendering.
type ChannelInfo struct {
	IsOnline     bool
	IsPaused     bool
	Username     string
	Duration     string
	Filesize     string
	Filename     string
	StreamedAt   string
	MaxDuration  string
	MaxFilesize  string
	CreatedAt    int64
	Logs         []string
	GlobalConfig *Config // for nested template to access $.Config
}

// VideoProgress represents the viewing progress of a video.
type VideoProgress struct {
	VideoPath       string  `json:"video_path"`
	CurrentTime     float64 `json:"current_time"`
	Duration        float64 `json:"duration"`
	PercentWatched  float64 `json:"percent_watched"`
	LastWatchedAt   int64   `json:"last_watched_at"`
	IsCompleted     bool    `json:"is_completed"`
}

// TranscodingJob represents a transcoding job in the queue.
type TranscodingJob struct {
	ID           string    `json:"id"`
	InputPath    string    `json:"input_path"`
	OutputPath   string    `json:"output_path"`
	Status       string    `json:"status"` // pending, processing, completed, failed
	Progress     float64   `json:"progress"`
	StartedAt    int64     `json:"started_at"`
	CompletedAt  int64     `json:"completed_at"`
	ErrorMessage string    `json:"error_message"`
	FileSize     int64     `json:"file_size"`
}

// TranscodingStatus represents the overall transcoding system status.
type TranscodingStatus struct {
	IsRunning       bool              `json:"is_running"`
	QueueSize       int               `json:"queue_size"`
	ActiveJobs      int               `json:"active_jobs"`
	CompletedJobs   int               `json:"completed_jobs"`
	FailedJobs      int               `json:"failed_jobs"`
	CurrentJob      *TranscodingJob   `json:"current_job"`
	RecentJobs      []TranscodingJob  `json:"recent_jobs"`
}

// Config holds the configuration for the application.
type Config struct {
	Version            string
	Username           string
	AdminUsername      string
	AdminPassword      string
	Framerate          int
	Resolution         int
	Pattern            string
	MaxDuration        int
	MaxFilesize        int
	Port               string
	Interval           int
	Cookies            string
	UserAgent          string
	Domain             string
	AutoDeleteWatched  bool   `json:"auto_delete_watched"`
	TranscodingEnabled bool   `json:"transcoding_enabled"`
	TranscodingCleanup bool   `json:"transcoding_cleanup"`
	TranscodingQuality string `json:"transcoding_quality"` // fast, medium, slow
}
