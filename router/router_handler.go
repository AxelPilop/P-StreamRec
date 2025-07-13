package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/teacat/chaturbate-dvr/config"
	"github.com/teacat/chaturbate-dvr/entity"
	"github.com/teacat/chaturbate-dvr/server"
	"github.com/teacat/chaturbate-dvr/transcoding"
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
	Pattern     string `form:"pattern"`
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
		// Use default pattern if none provided
		pattern := req.Pattern
		if pattern == "" {
			pattern = server.Config.Pattern
		}
		
		server.Manager.CreateChannel(&entity.ChannelConfig{
			IsPaused:    false,
			Username:    strings.TrimSpace(username),
			Framerate:   req.Framerate,
			Resolution:  req.Resolution,
			Pattern:     pattern,
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
	Cookies            string `form:"cookies"`
	UserAgent          string `form:"user_agent"`
	Pattern            string `form:"pattern"`
	AutoDeleteWatched  bool   `form:"auto_delete_watched"`
	TranscodingEnabled bool   `form:"transcoding_enabled"`
	TranscodingCleanup bool   `form:"transcoding_cleanup"`
	TranscodingQuality string `form:"transcoding_quality"`
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
	server.Config.AutoDeleteWatched = req.AutoDeleteWatched
	server.Config.TranscodingEnabled = req.TranscodingEnabled
	server.Config.TranscodingCleanup = req.TranscodingCleanup
	server.Config.TranscodingQuality = req.TranscodingQuality

	// Save settings persistently
	if err := config.SavePersistentSettings(server.Config); err != nil {
		// Log error but don't fail the request
		fmt.Printf("Warning: Failed to save settings: %v\n", err)
	}
	
	// Restart transcoding service if settings changed
	if server.TranscodingService != nil {
		if !req.TranscodingEnabled {
			server.TranscodingService.Stop()
			server.TranscodingService = nil
		}
	} else if req.TranscodingEnabled {
		server.TranscodingService = transcoding.NewTranscodingService(2)
		server.TranscodingService.Start()
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
	Progress     *entity.VideoProgress
}

// StreamerVideoData represents videos grouped by streamer.
type StreamerVideoData struct {
	Username   string
	VideoCount int
	Videos     []VideoInfo
	TotalSize  int64
	TotalSizeFormatted string
}

// VideoList renders a page with available videos.
func VideoList(c *gin.Context) {
	videos, err := getVideoFiles()
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to list videos: %w", err))
		return
	}

	// Load video progress for each video
	loadVideoProgress(&videos)

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

	// Set appropriate headers for video streaming with mpegts.js
	c.Header("Content-Type", "video/mp2t")
	c.Header("Accept-Ranges", "bytes")
	c.Header("Cache-Control", "no-cache")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Range, Content-Type")
	c.File(fullPath)
}

// ServeVideoCompatible serves video files with better browser compatibility.
func ServeVideoCompatible(c *gin.Context) {
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

	// Set headers for better compatibility
	c.Header("Content-Type", "video/mp4")
	c.Header("Accept-Ranges", "bytes")
	c.Header("Cache-Control", "public, max-age=3600")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Range")
	
	c.File(fullPath)
}

// getVideoFiles scans the videos directory and returns a list of video files.
// Prioritizes .mp4 files over .ts files for better browser compatibility.
func getVideoFiles() ([]VideoInfo, error) {
	var videos []VideoInfo
	videoMap := make(map[string]VideoInfo) // Map by base filename (without extension)
	
	err := filepath.Walk("videos", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".ts" || ext == ".mp4" {
				// Extract username from filename
				username := extractUsernameFromPath(path)
				
				// Get base filename without extension
				baseFilename := strings.TrimSuffix(info.Name(), filepath.Ext(info.Name()))
				
				videoInfo := VideoInfo{
					Name:         info.Name(),
					Path:         path,
					Size:         info.Size(),
					SizeFormatted: formatFileSize(info.Size()),
					ModTime:      info.ModTime(),
					Username:     username,
					Progress:     nil, // Will be loaded later
				}
				
				// Check if we already have this video
				if existing, exists := videoMap[baseFilename]; exists {
					// Prefer .mp4 over .ts
					if ext == ".mp4" && strings.HasSuffix(existing.Path, ".ts") {
						videoMap[baseFilename] = videoInfo
					}
					// If existing is .mp4 and current is .ts, keep existing
				} else {
					videoMap[baseFilename] = videoInfo
				}
			}
		}
		return nil
	})

	// Convert map to slice
	for _, video := range videoMap {
		videos = append(videos, video)
	}

	return videos, err
}

// extractUsernameFromPath extracts the username from the video file path.
func extractUsernameFromPath(path string) string {
	filename := filepath.Base(path)
	// Remove extension (both .ts and .mp4)
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
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

// DeleteVideo deletes a video file.
func DeleteVideo(c *gin.Context) {
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

	// Delete the file
	if err := os.Remove(fullPath); err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to delete video: %w", err))
		return
	}

	// Also remove progress data for this video
	deleteVideoProgress(videoPath)

	c.JSON(200, gin.H{"success": true})
}

// UpdateVideoProgress updates the viewing progress of a video.
func UpdateVideoProgress(c *gin.Context) {
	var progress entity.VideoProgress
	if err := c.ShouldBindJSON(&progress); err != nil {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("bind: %w", err))
		return
	}

	// Calculate percentage
	if progress.Duration > 0 {
		progress.PercentWatched = (progress.CurrentTime / progress.Duration) * 100
		progress.IsCompleted = progress.PercentWatched >= 99.0 // Consider 99% as completed
	}

	progress.LastWatchedAt = time.Now().Unix()

	// Save progress
	if err := saveVideoProgress(&progress); err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to save progress: %w", err))
		return
	}

	// Auto-delete if enabled and video is completed
	if server.Config.AutoDeleteWatched && progress.IsCompleted {
		fullPath := filepath.Join("videos", progress.VideoPath)
		if err := os.Remove(fullPath); err == nil {
			deleteVideoProgress(progress.VideoPath)
		}
	}

	c.JSON(200, gin.H{"success": true})
}

// GetVideoProgress returns the viewing progress of a video.
func GetVideoProgress(c *gin.Context) {
	videoPath := c.Param("filepath")
	if videoPath == "" {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// Remove leading slash
	if videoPath[0] == '/' {
		videoPath = videoPath[1:]
	}

	progress := loadVideoProgressForPath(videoPath)
	if progress == nil {
		c.JSON(200, gin.H{"progress": nil})
		return
	}

	c.JSON(200, gin.H{"progress": progress})
}

// groupVideosByStreamer groups videos by streamer username.
func groupVideosByStreamer(videos []VideoInfo) []StreamerVideoData {
	streamerMap := make(map[string][]VideoInfo)
	
	for _, video := range videos {
		streamerMap[video.Username] = append(streamerMap[video.Username], video)
	}
	
	var streamers []StreamerVideoData
	for username, videos := range streamerMap {
		// Sort videos by modification time (newest first)
		sort.Slice(videos, func(i, j int) bool {
			return videos[i].ModTime.After(videos[j].ModTime)
		})
		
		// Calculate total size
		var totalSize int64
		for _, video := range videos {
			totalSize += video.Size
		}
		
		streamers = append(streamers, StreamerVideoData{
			Username:           username,
			VideoCount:         len(videos),
			Videos:             videos,
			TotalSize:          totalSize,
			TotalSizeFormatted: formatFileSize(totalSize),
		})
	}
	
	// Sort streamers by username
	sort.Slice(streamers, func(i, j int) bool {
		return streamers[i].Username < streamers[j].Username
	})
	
	return streamers
}

// loadVideoProgress loads viewing progress for all videos.
func loadVideoProgress(videos *[]VideoInfo) {
	progressData := loadAllVideoProgress()
	
	for i := range *videos {
		if progress, exists := progressData[(*videos)[i].Path]; exists {
			(*videos)[i].Progress = &progress
		}
	}
}

// loadVideoProgressForPath loads viewing progress for a specific video path.
func loadVideoProgressForPath(videoPath string) *entity.VideoProgress {
	progressData := loadAllVideoProgress()
	if progress, exists := progressData[videoPath]; exists {
		return &progress
	}
	return nil
}

// loadAllVideoProgress loads all video progress data from file.
func loadAllVideoProgress() map[string]entity.VideoProgress {
	progressFile := "./conf/video_progress.json"
	progressData := make(map[string]entity.VideoProgress)
	
	if data, err := os.ReadFile(progressFile); err == nil {
		json.Unmarshal(data, &progressData)
	}
	
	return progressData
}

// saveVideoProgress saves viewing progress for a video.
func saveVideoProgress(progress *entity.VideoProgress) error {
	progressData := loadAllVideoProgress()
	progressData[progress.VideoPath] = *progress
	
	// Ensure conf directory exists
	if err := os.MkdirAll("./conf", 0755); err != nil {
		return err
	}
	
	data, err := json.MarshalIndent(progressData, "", "  ")
	if err != nil {
		return err
	}
	
	return os.WriteFile("./conf/video_progress.json", data, 0644)
}

// deleteVideoProgress deletes viewing progress for a video.
func deleteVideoProgress(videoPath string) {
	progressData := loadAllVideoProgress()
	delete(progressData, videoPath)
	
	// Ensure conf directory exists
	os.MkdirAll("./conf", 0755)
	
	data, _ := json.MarshalIndent(progressData, "", "  ")
	os.WriteFile("./conf/video_progress.json", data, 0644)
}

// TranscodeVideo adds a video to the transcoding queue.
func TranscodeVideo(c *gin.Context) {
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
	
	// Check if file exists and is a .ts file
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	if !strings.HasSuffix(strings.ToLower(fullPath), ".ts") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only .ts files can be transcoded"})
		return
	}

	// Generate output path (.mp4)
	outputPath := strings.TrimSuffix(fullPath, ".ts") + ".mp4"

	// Check if transcoding service is available
	if server.TranscodingService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Transcoding service not available"})
		return
	}

	// Add job to transcoding queue
	job, err := server.TranscodingService.AddJob(fullPath, outputPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"success": true,
		"job_id":  job.ID,
		"message": "Video added to transcoding queue",
	})
}

// GetTranscodingStatus returns the current transcoding status.
func GetTranscodingStatus(c *gin.Context) {
	if server.TranscodingService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Transcoding service not available"})
		return
	}

	status := server.TranscodingService.GetStatus()
	c.JSON(200, gin.H{"status": status})
}

// GetTranscodingJob returns information about a specific transcoding job.
func GetTranscodingJob(c *gin.Context) {
	jobID := c.Param("job_id")
	if jobID == "" {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if server.TranscodingService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Transcoding service not available"})
		return
	}

	job, exists := server.TranscodingService.GetJob(jobID)
	if !exists {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	c.JSON(200, gin.H{"job": job})
}

// CleanupTranscodedFiles removes .ts files that have been successfully transcoded to .mp4.
func CleanupTranscodedFiles(c *gin.Context) {
	if !server.Config.TranscodingCleanup {
		c.JSON(http.StatusForbidden, gin.H{"error": "Transcoding cleanup is disabled"})
		return
	}

	var cleanedFiles []string
	var errors []string

	err := filepath.Walk("videos", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(strings.ToLower(path), ".ts") {
			// Check if corresponding .mp4 file exists
			mp4Path := strings.TrimSuffix(path, ".ts") + ".mp4"
			if _, err := os.Stat(mp4Path); err == nil {
				// .mp4 file exists, check if it's newer than .ts file
				mp4Info, err := os.Stat(mp4Path)
				if err == nil && mp4Info.ModTime().After(info.ModTime()) {
					// Remove the .ts file
					if err := os.Remove(path); err != nil {
						errors = append(errors, fmt.Sprintf("Failed to remove %s: %v", path, err))
					} else {
						cleanedFiles = append(cleanedFiles, path)
						// Also remove progress data for this file
						deleteVideoProgress(path)
					}
				}
			}
		}
		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"success":        true,
		"cleaned_files":  cleanedFiles,
		"errors":         errors,
		"files_cleaned":  len(cleanedFiles),
	})
}

// ScheduleTranscoding automatically schedules transcoding for old .ts files.
func ScheduleTranscoding(c *gin.Context) {
	if server.TranscodingService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Transcoding service not available"})
		return
	}

	var scheduledJobs []string
	var errors []string
	
	// Find .ts files that are older than 1 hour and don't have corresponding .mp4 files
	cutoffTime := time.Now().Add(-1 * time.Hour)

	err := filepath.Walk("videos", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(strings.ToLower(path), ".ts") {
			// Check if file is older than cutoff time
			if info.ModTime().Before(cutoffTime) {
				// Check if corresponding .mp4 file doesn't exist
				mp4Path := strings.TrimSuffix(path, ".ts") + ".mp4"
				if _, err := os.Stat(mp4Path); os.IsNotExist(err) {
					// Schedule for transcoding
					job, err := server.TranscodingService.AddJob(path, mp4Path)
					if err != nil {
						errors = append(errors, fmt.Sprintf("Failed to schedule %s: %v", path, err))
					} else {
						scheduledJobs = append(scheduledJobs, job.ID)
					}
				}
			}
		}
		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"success":        true,
		"scheduled_jobs": scheduledJobs,
		"errors":         errors,
		"jobs_scheduled": len(scheduledJobs),
	})
}
