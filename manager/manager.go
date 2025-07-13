package manager

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/r3labs/sse/v2"
	"github.com/teacat/chaturbate-dvr/channel"
	"github.com/teacat/chaturbate-dvr/entity"
	"github.com/teacat/chaturbate-dvr/router/view"
)

// Manager is responsible for managing channels and their states.
type Manager struct {
	Channels sync.Map
	SSE      *sse.Server
}

// New initializes a new Manager instance with an SSE server.
func New() (*Manager, error) {

	server := sse.New()
	server.SplitData = true

	updateStream := server.CreateStream("updates")
	updateStream.AutoReplay = false

	manager := &Manager{
		SSE: server,
	}
	
	// Start health monitoring
	go manager.StartHealthMonitoring()

	return manager, nil
}

// SaveConfig saves the current channels and state to a JSON file.
func (m *Manager) SaveConfig() error {
	var config []*entity.ChannelConfig

	m.Channels.Range(func(key, value any) bool {
		config = append(config, value.(*channel.Channel).Config)
		return true
	})

	b, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.MkdirAll("./conf", 0777); err != nil {
		return fmt.Errorf("mkdir all conf: %w", err)
	}
	if err := os.WriteFile("./conf/channels.json", b, 0777); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// LoadConfig loads the channels from JSON and starts them.
func (m *Manager) LoadConfig() error {
	b, err := os.ReadFile("./conf/channels.json")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	var config []*entity.ChannelConfig
	if err := json.Unmarshal(b, &config); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	for i, conf := range config {
		ch := channel.New(conf)
		m.Channels.Store(conf.Username, ch)

		if ch.Config.IsPaused {
			ch.Info("channel was paused, waiting for resume")
			continue
		}
		go ch.Resume(i)
	}
	return nil
}

// CreateChannel starts monitoring an M3U8 stream
func (m *Manager) CreateChannel(conf *entity.ChannelConfig, shouldSave bool) error {
	conf.Sanitize()
	ch := channel.New(conf)

	// prevent duplicate channels
	_, ok := m.Channels.Load(conf.Username)
	if ok {
		return fmt.Errorf("channel %s already exists", conf.Username)
	}
	m.Channels.Store(conf.Username, ch)

	go ch.Resume(0)

	if shouldSave {
		if err := m.SaveConfig(); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}
	return nil
}

// StopChannel stops the channel.
func (m *Manager) StopChannel(username string) error {
	thing, ok := m.Channels.Load(username)
	if !ok {
		return nil
	}
	thing.(*channel.Channel).Stop()
	m.Channels.Delete(username)

	if err := m.SaveConfig(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

// PauseChannel pauses the channel.
func (m *Manager) PauseChannel(username string) error {
	thing, ok := m.Channels.Load(username)
	if !ok {
		return nil
	}
	thing.(*channel.Channel).Pause()

	if err := m.SaveConfig(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

// ResumeChannel resumes the channel.
func (m *Manager) ResumeChannel(username string) error {
	thing, ok := m.Channels.Load(username)
	if !ok {
		return nil
	}
	thing.(*channel.Channel).Resume(0)

	if err := m.SaveConfig(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

// ChannelInfo returns a list of channel information for the web UI.
func (m *Manager) ChannelInfo() []*entity.ChannelInfo {
	var channels []*entity.ChannelInfo

	// Iterate over the channels and append their information to the slice
	m.Channels.Range(func(key, value any) bool {
		channels = append(channels, value.(*channel.Channel).ExportInfo())
		return true
	})

	sort.Slice(channels, func(i, j int) bool {
		// First priority: Online channels
		if channels[i].IsOnline != channels[j].IsOnline {
			return channels[i].IsOnline
		}
		// Second priority: Alphabetical order by username
		return strings.ToLower(channels[i].Username) < strings.ToLower(channels[j].Username)
	})

	return channels
}

// Publish sends an SSE event to the specified channel.
func (m *Manager) Publish(evt entity.Event, info *entity.ChannelInfo) {
	switch evt {
	case entity.EventUpdate:
		var b bytes.Buffer
		if err := view.InfoTpl.ExecuteTemplate(&b, "channel_info", info); err != nil {
			fmt.Println("Error executing template:", err)
			return
		}
		m.SSE.Publish("updates", &sse.Event{
			Event: []byte(info.Username + "-info"),
			Data:  b.Bytes(),
		})
		
		// Also publish status update for compact view
		var statusBuffer bytes.Buffer
		if err := view.InfoTpl.ExecuteTemplate(&statusBuffer, "channel_status", info); err != nil {
			fmt.Println("Error executing status template:", err)
			return
		}
		m.SSE.Publish("updates", &sse.Event{
			Event: []byte(info.Username + "-status"),
			Data:  statusBuffer.Bytes(),
		})
	case entity.EventLog:
		m.SSE.Publish("updates", &sse.Event{
			Event: []byte(info.Username + "-log"),
			Data:  []byte(strings.Join(info.Logs, "\n")),
		})
	}
}

// Subscriber handles SSE subscriptions for the specified channel.
func (m *Manager) Subscriber(w http.ResponseWriter, r *http.Request) {
	m.SSE.ServeHTTP(w, r)
}

// StartHealthMonitoring periodically checks channel health and restarts failed monitors
func (m *Manager) StartHealthMonitoring() {
	ticker := time.NewTicker(2 * time.Minute) // Check every 2 minutes
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.checkChannelHealth()
		}
	}
}

// checkChannelHealth checks for failed channels and restarts them
func (m *Manager) checkChannelHealth() {
	now := time.Now()
	
	m.Channels.Range(func(key, value any) bool {
		ch := value.(*channel.Channel)
		username := key.(string)
		
		// Skip paused channels
		if ch.Config.IsPaused {
			return true
		}
		
		// Check if monitor should be active but isn't responding
		timeSinceHeartbeat := now.Sub(ch.LastHeartbeat)
		
		if ch.MonitorActive && timeSinceHeartbeat > 10*time.Minute {
			ch.Error("HEALTH_CHECK: Monitor appears stuck (last heartbeat: %v ago), attempting restart", timeSinceHeartbeat)
			
			// Try to restart the channel
			go func(channel *channel.Channel, name string) {
				// Stop the old monitor
				channel.Stop()
				time.Sleep(5 * time.Second) // Give it time to clean up
				
				// Restart monitoring
				channel.Info("HEALTH_CHECK: Restarting monitor due to health check failure")
				channel.Resume(0)
			}(ch, username)
			
		} else if !ch.MonitorActive && !ch.Config.IsPaused {
			ch.Error("HEALTH_CHECK: Monitor should be active but isn't, attempting restart")
			
			// Restart monitoring for channels that should be running
			go func(channel *channel.Channel) {
				channel.Info("HEALTH_CHECK: Starting monitor due to inactive state")
				channel.Resume(0)
			}(ch)
		}
		
		return true
	})
}
