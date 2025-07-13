package converter

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ConvertTSToMP4 converts a .ts file to .mp4 format using FFmpeg
func ConvertTSToMP4(tsFilePath string) error {
	// Check if input file exists
	if _, err := os.Stat(tsFilePath); os.IsNotExist(err) {
		return fmt.Errorf("input file does not exist: %s", tsFilePath)
	}

	// Generate output path
	mp4FilePath := strings.TrimSuffix(tsFilePath, ".ts") + ".mp4"

	// Check if output file already exists
	if _, err := os.Stat(mp4FilePath); err == nil {
		return fmt.Errorf("output file already exists: %s", mp4FilePath)
	}

	// Check if FFmpeg is available
	if !isFFmpegAvailable() {
		return fmt.Errorf("FFmpeg not found - please install FFmpeg to use conversion")
	}

	// FFmpeg command to convert .ts to .mp4
	cmd := exec.Command("ffmpeg",
		"-i", tsFilePath,
		"-c", "copy", // Copy streams without re-encoding for speed
		"-avoid_negative_ts", "make_zero",
		"-fflags", "+genpts",
		"-y", // Overwrite output file
		mp4FilePath,
	)

	// Run the command
	if err := cmd.Run(); err != nil {
		// Clean up partial file if conversion failed
		os.Remove(mp4FilePath)
		return fmt.Errorf("FFmpeg conversion failed: %v", err)
	}

	// Verify the output file was created and has content
	if info, err := os.Stat(mp4FilePath); err != nil || info.Size() == 0 {
		os.Remove(mp4FilePath)
		return fmt.Errorf("conversion failed: output file is empty or missing")
	}

	return nil
}

// isFFmpegAvailable checks if FFmpeg is available in the system
func isFFmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// ShouldConvertFile checks if a .ts file should be converted based on its modification date
func ShouldConvertFile(filePath string) bool {
	// Get file info
	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}

	// Check if file is from previous day (not today)
	now := time.Now()
	fileDate := info.ModTime()
	
	// Get start of today (00:00:00)
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	
	// File should be converted if it's from before today
	return fileDate.Before(startOfToday)
}

// ConvertOldTSFiles scans for .ts files and converts old ones to .mp4
func ConvertOldTSFiles() error {
	err := filepath.Walk("videos", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Only process .ts files
		if !strings.HasSuffix(strings.ToLower(path), ".ts") {
			return nil
		}

		// Check if file should be converted
		if !ShouldConvertFile(path) {
			return nil
		}

		// Check if MP4 version already exists
		mp4Path := strings.TrimSuffix(path, ".ts") + ".mp4"
		if _, err := os.Stat(mp4Path); err == nil {
			// MP4 already exists, remove the .ts file
			fmt.Printf("MP4 already exists for %s, removing TS file\n", path)
			if err := os.Remove(path); err != nil {
				fmt.Printf("Warning: Failed to remove %s: %v\n", path, err)
			}
			return nil
		}

		// Convert the file
		fmt.Printf("Converting %s to MP4...\n", path)
		if err := ConvertTSToMP4(path); err != nil {
			fmt.Printf("Failed to convert %s: %v\n", path, err)
			return nil // Continue with other files
		}

		// Remove the original .ts file after successful conversion
		fmt.Printf("Conversion successful, removing original TS file: %s\n", path)
		if err := os.Remove(path); err != nil {
			fmt.Printf("Warning: Failed to remove original TS file %s: %v\n", path, err)
		}

		return nil
	})

	return err
}

// GetMP4Files returns a list of all .mp4 files in the videos directory
func GetMP4Files() ([]string, error) {
	var mp4Files []string
	
	err := filepath.Walk("videos", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(strings.ToLower(path), ".mp4") {
			mp4Files = append(mp4Files, path)
		}
		return nil
	})

	return mp4Files, err
}