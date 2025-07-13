package transcoding

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/teacat/chaturbate-dvr/entity"
)

// TranscodingService handles the transcoding of video files.
type TranscodingService struct {
	mu           sync.RWMutex
	jobs         map[string]*entity.TranscodingJob
	queue        chan *entity.TranscodingJob
	ctx          context.Context
	cancel       context.CancelFunc
	isRunning    bool
	completedJobs int
	failedJobs   int
	maxWorkers   int
}

// NewTranscodingService creates a new transcoding service.
func NewTranscodingService(maxWorkers int) *TranscodingService {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &TranscodingService{
		jobs:       make(map[string]*entity.TranscodingJob),
		queue:      make(chan *entity.TranscodingJob, 100),
		ctx:        ctx,
		cancel:     cancel,
		maxWorkers: maxWorkers,
	}
}

// Start starts the transcoding service workers.
func (ts *TranscodingService) Start() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	
	if ts.isRunning {
		return
	}
	
	ts.isRunning = true
	
	// Start worker goroutines
	for i := 0; i < ts.maxWorkers; i++ {
		go ts.worker()
	}
	
	// Start job monitoring
	go ts.monitorJobs()
}

// Stop stops the transcoding service.
func (ts *TranscodingService) Stop() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	
	if !ts.isRunning {
		return
	}
	
	ts.isRunning = false
	ts.cancel()
	close(ts.queue)
}

// AddJob adds a new transcoding job to the queue.
func (ts *TranscodingService) AddJob(inputPath, outputPath string) (*entity.TranscodingJob, error) {
	// Check if input file exists
	if _, err := os.Stat(inputPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("input file does not exist: %s", inputPath)
	}
	
	// Check if output file already exists
	if _, err := os.Stat(outputPath); err == nil {
		return nil, fmt.Errorf("output file already exists: %s", outputPath)
	}
	
	// Create output directory if it doesn't exist
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}
	
	// Get file size
	fileInfo, err := os.Stat(inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}
	
	job := &entity.TranscodingJob{
		ID:         generateJobID(),
		InputPath:  inputPath,
		OutputPath: outputPath,
		Status:     "pending",
		Progress:   0,
		StartedAt:  0,
		CompletedAt: 0,
		FileSize:   fileInfo.Size(),
	}
	
	ts.mu.Lock()
	ts.jobs[job.ID] = job
	ts.mu.Unlock()
	
	// Add to queue
	select {
	case ts.queue <- job:
		return job, nil
	case <-ts.ctx.Done():
		return nil, fmt.Errorf("transcoding service is stopped")
	}
}

// GetJob returns a job by ID.
func (ts *TranscodingService) GetJob(id string) (*entity.TranscodingJob, bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	
	job, exists := ts.jobs[id]
	return job, exists
}

// GetStatus returns the current status of the transcoding service.
func (ts *TranscodingService) GetStatus() *entity.TranscodingStatus {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	
	status := &entity.TranscodingStatus{
		IsRunning:     ts.isRunning,
		QueueSize:     len(ts.queue),
		ActiveJobs:    ts.countActiveJobs(),
		CompletedJobs: ts.completedJobs,
		FailedJobs:    ts.failedJobs,
		CurrentJob:    ts.getCurrentJob(),
		RecentJobs:    ts.getRecentJobs(),
	}
	
	return status
}

// worker processes transcoding jobs from the queue.
func (ts *TranscodingService) worker() {
	for {
		select {
		case job, ok := <-ts.queue:
			if !ok {
				return // Queue closed
			}
			
			ts.processJob(job)
			
		case <-ts.ctx.Done():
			return
		}
	}
}

// processJob processes a single transcoding job.
func (ts *TranscodingService) processJob(job *entity.TranscodingJob) {
	// Update job status
	ts.mu.Lock()
	job.Status = "processing"
	job.StartedAt = time.Now().Unix()
	ts.mu.Unlock()
	
	// Check if FFmpeg is available
	if !ts.isFFmpegAvailable() {
		ts.mu.Lock()
		job.Status = "failed"
		job.ErrorMessage = "FFmpeg not found - please install FFmpeg to use transcoding"
		job.CompletedAt = time.Now().Unix()
		ts.failedJobs++
		ts.mu.Unlock()
		return
	}
	
	// Run FFmpeg
	err := ts.runFFmpeg(job)
	
	ts.mu.Lock()
	defer ts.mu.Unlock()
	
	if err != nil {
		job.Status = "failed"
		job.ErrorMessage = err.Error()
		job.CompletedAt = time.Now().Unix()
		ts.failedJobs++
	} else {
		job.Status = "completed"
		job.Progress = 100
		job.CompletedAt = time.Now().Unix()
		ts.completedJobs++
	}
}

// runFFmpeg runs the FFmpeg command for transcoding.
func (ts *TranscodingService) runFFmpeg(job *entity.TranscodingJob) error {
	// FFmpeg command to convert .ts to .mp4
	args := []string{
		"-i", job.InputPath,
		"-c:v", "libx264",
		"-preset", "fast",
		"-crf", "23",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
		"-y", // Overwrite output file
		job.OutputPath,
	}
	
	cmd := exec.CommandContext(ts.ctx, "ffmpeg", args...)
	
	// Set up progress tracking
	cmd.Stderr = &progressWriter{
		job: job,
		ts:  ts,
	}
	
	return cmd.Run()
}

// isFFmpegAvailable checks if FFmpeg is available in the system.
func (ts *TranscodingService) isFFmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// countActiveJobs counts the number of active jobs.
func (ts *TranscodingService) countActiveJobs() int {
	count := 0
	for _, job := range ts.jobs {
		if job.Status == "processing" {
			count++
		}
	}
	return count
}

// getCurrentJob returns the currently processing job.
func (ts *TranscodingService) getCurrentJob() *entity.TranscodingJob {
	for _, job := range ts.jobs {
		if job.Status == "processing" {
			return job
		}
	}
	return nil
}

// getRecentJobs returns the 10 most recent jobs.
func (ts *TranscodingService) getRecentJobs() []entity.TranscodingJob {
	var jobs []entity.TranscodingJob
	
	for _, job := range ts.jobs {
		if job.Status == "completed" || job.Status == "failed" {
			jobs = append(jobs, *job)
		}
	}
	
	// Sort by completion time (most recent first)
	for i := 0; i < len(jobs)-1; i++ {
		for j := i + 1; j < len(jobs); j++ {
			if jobs[i].CompletedAt < jobs[j].CompletedAt {
				jobs[i], jobs[j] = jobs[j], jobs[i]
			}
		}
	}
	
	// Return up to 10 jobs
	if len(jobs) > 10 {
		jobs = jobs[:10]
	}
	
	return jobs
}

// monitorJobs monitors and cleans up old jobs.
func (ts *TranscodingService) monitorJobs() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			ts.cleanupOldJobs()
		case <-ts.ctx.Done():
			return
		}
	}
}

// cleanupOldJobs removes old completed/failed jobs from memory.
func (ts *TranscodingService) cleanupOldJobs() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	
	now := time.Now().Unix()
	oneHourAgo := now - 3600
	
	for id, job := range ts.jobs {
		if (job.Status == "completed" || job.Status == "failed") && job.CompletedAt < oneHourAgo {
			delete(ts.jobs, id)
		}
	}
}

// generateJobID generates a unique job ID.
func generateJobID() string {
	return fmt.Sprintf("job_%d", time.Now().UnixNano())
}

// progressWriter captures FFmpeg progress output.
type progressWriter struct {
	job *entity.TranscodingJob
	ts  *TranscodingService
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	// Parse FFmpeg progress output
	output := string(p)
	
	// Look for time= in the output to estimate progress
	if strings.Contains(output, "time=") {
		// Simple progress estimation - in a real implementation,
		// you might want to parse the duration first and calculate percentage
		pw.ts.mu.Lock()
		if pw.job.Progress < 95 {
			pw.job.Progress += 5
		}
		pw.ts.mu.Unlock()
	}
	
	return len(p), nil
}

// SaveJobsToFile saves the current jobs to a JSON file.
func (ts *TranscodingService) SaveJobsToFile(filename string) error {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	
	// Create conf directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return err
	}
	
	data, err := json.MarshalIndent(ts.jobs, "", "  ")
	if err != nil {
		return err
	}
	
	return os.WriteFile(filename, data, 0644)
}

// LoadJobsFromFile loads jobs from a JSON file.
func (ts *TranscodingService) LoadJobsFromFile(filename string) error {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return nil // File doesn't exist, no jobs to load
	}
	
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	
	ts.mu.Lock()
	defer ts.mu.Unlock()
	
	return json.Unmarshal(data, &ts.jobs)
}