package router

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/teacat/chaturbate-dvr/config"
	"github.com/teacat/chaturbate-dvr/entity"
	"github.com/teacat/chaturbate-dvr/server"
)

// IndexData represents the data structure for the index page.
type IndexData struct {
	Config   *entity.Config
	Channels []*entity.ChannelInfo
}

// Index renders the index page with channel information.
func Index(c *gin.Context) {
	c.HTML(200, "index.html", &IndexData{
		Config:   server.Config,
		Channels: server.Manager.ChannelInfo(),
	})
}

// CreateChannelRequest represents the request body for creating a channel.
type CreateChannelRequest struct {
	Username    string `form:"username" binding:"required"`
	Framerate   int    `form:"framerate" binding:"required"`
	Resolution  int    `form:"resolution" binding:"required"`
	Pattern     string `form:"pattern" binding:"required"`
	MaxDuration int    `form:"max_duration"`
	MaxFilesize int    `form:"max_filesize"`
}

// CreateChannel creates a new channel.
func CreateChannel(c *gin.Context) {
	var req *CreateChannelRequest
	if err := c.Bind(&req); err != nil {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("bind: %w", err))
		return
	}

	for _, username := range strings.Split(req.Username, ",") {
		server.Manager.CreateChannel(&entity.ChannelConfig{
			IsPaused:    false,
			Username:    username,
			Framerate:   req.Framerate,
			Resolution:  req.Resolution,
			Pattern:     req.Pattern,
			MaxDuration: req.MaxDuration,
			MaxFilesize: req.MaxFilesize,
			CreatedAt:   time.Now().Unix(),
		}, true)
	}
	c.Redirect(http.StatusFound, "/")
}

// StopChannel stops a channel.
func StopChannel(c *gin.Context) {
	server.Manager.StopChannel(c.Param("username"))

	c.Redirect(http.StatusFound, "/")
}

// PauseChannel pauses a channel.
func PauseChannel(c *gin.Context) {
	server.Manager.PauseChannel(c.Param("username"))

	c.Redirect(http.StatusFound, "/")
}

// ResumeChannel resumes a paused channel.
func ResumeChannel(c *gin.Context) {
	server.Manager.ResumeChannel(c.Param("username"))

	c.Redirect(http.StatusFound, "/")
}

// Updates handles the SSE connection for updates.
func Updates(c *gin.Context) {
	server.Manager.Subscriber(c.Writer, c.Request)
}

// UpdateConfigRequest represents the request body for updating configuration.
type UpdateConfigRequest struct {
	Cookies   string `form:"cookies"`
	UserAgent string `form:"user_agent"`
	Pattern   string `form:"pattern"`
}

// UpdateConfig updates the server configuration.
func UpdateConfig(c *gin.Context) {
	var req *UpdateConfigRequest
	if err := c.Bind(&req); err != nil {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("bind: %w", err))
		return
	}

	server.Config.Cookies = req.Cookies
	server.Config.UserAgent = req.UserAgent
	server.Config.Pattern = req.Pattern

	// Save settings persistently
	if err := config.SavePersistentSettings(server.Config); err != nil {
		// Log error but don't fail the request
		fmt.Printf("Warning: Failed to save settings: %v\n", err)
	}

	c.Redirect(http.StatusFound, "/")
}

// VideoInfo represents information about a video file.
type VideoInfo struct {
	Name         string
	Path         string
	Size         int64
	SizeFormatted string
	ModTime      time.Time
	Username     string
}

// VideoList renders a page with available videos.
func VideoList(c *gin.Context) {
	videos, err := getVideoFiles()
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to list videos: %w", err))
		return
	}

	c.HTML(200, "videos.html", gin.H{
		"Videos": videos,
		"Config": server.Config,
	})
}

// ServeVideo serves video files for streaming.
func ServeVideo(c *gin.Context) {
	videoPath := c.Param("filepath")
	if videoPath == "" {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// Remove leading slash
	if videoPath[0] == '/' {
		videoPath = videoPath[1:]
	}

	// Construct full path
	fullPath := filepath.Join("videos", videoPath)
	
	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	// Set appropriate headers for video streaming
	c.Header("Content-Type", "video/mp2t")
	c.Header("Accept-Ranges", "bytes")
	c.File(fullPath)
}

// getVideoFiles scans the videos directory and returns a list of video files.
func getVideoFiles() ([]VideoInfo, error) {
	var videos []VideoInfo
	
	err := filepath.Walk("videos", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(strings.ToLower(path), ".ts") {
			// Extract username from filename
			username := extractUsernameFromPath(path)
			
			videos = append(videos, VideoInfo{
				Name:         info.Name(),
				Path:         path,
				Size:         info.Size(),
				SizeFormatted: formatFileSize(info.Size()),
				ModTime:      info.ModTime(),
				Username:     username,
			})
		}
		return nil
	})

	return videos, err
}

// extractUsernameFromPath extracts the username from the video file path.
func extractUsernameFromPath(path string) string {
	filename := filepath.Base(path)
	// Remove extension
	name := strings.TrimSuffix(filename, ".ts")
	// Split by underscore and take the first part as username
	parts := strings.Split(name, "_")
	if len(parts) > 0 {
		return parts[0]
	}
	return "unknown"
}

// formatFileSize formats file size in human readable format.
func formatFileSize(size int64) string {
	if size >= 1073741824 {
		return fmt.Sprintf("%.2f GB", float64(size)/1073741824)
	} else if size >= 1048576 {
		return fmt.Sprintf("%.2f MB", float64(size)/1048576)
	} else if size >= 1024 {
		return fmt.Sprintf("%.2f KB", float64(size)/1024)
	} else {
		return fmt.Sprintf("%d B", size)
	}
}
