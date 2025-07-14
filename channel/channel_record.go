package channel

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/teacat/chaturbate-dvr/chaturbate"
	"github.com/teacat/chaturbate-dvr/internal"
	"github.com/teacat/chaturbate-dvr/server"
)

// Monitor starts monitoring the channel for live streams and records them.
func (ch *Channel) Monitor() {
	// Add panic recovery to prevent goroutine crashes
	defer func() {
		if r := recover(); r != nil {
			ch.Error("PANIC in Monitor goroutine: %v", r)
		}
	}()

	client := chaturbate.NewClient()
	ch.Info("starting to record `%s`", ch.Config.Username)

	// Create a new context with a cancel function,
	// the CancelFunc will be stored in the channel's CancelFunc field
	// and will be called by `Pause` or `Stop` functions
	ctx, _ := ch.WithCancel(context.Background())

	// Add heartbeat logging
	heartbeatTicker := time.NewTicker(5 * time.Minute)
	defer heartbeatTicker.Stop()

	// Start heartbeat goroutine
	go func() {
		for {
			select {
			case <-heartbeatTicker.C:
				ch.LastHeartbeat = time.Now()
				ch.Info("HEARTBEAT: monitor active, checking for stream...")
			case <-ctx.Done():
				return
			}
		}
	}()

	var err error
	retryCount := 0

	for {
		if err = ctx.Err(); err != nil {
			ch.Info("monitor stopped due to context cancellation")
			break
		}

		pipeline := func() error {
			return ch.RecordStream(ctx, client)
		}
		onRetry := func(attempt uint, err error) {
			retryCount++
			ch.UpdateOnlineStatus(false)

			// Log only every 10th retry for offline channels to reduce spam
			shouldLog := retryCount <= 5 || retryCount%10 == 0

			if errors.Is(err, internal.ErrChannelOffline) || errors.Is(err, internal.ErrPrivateStream) {
				if shouldLog {
					ch.Info("channel is offline or private, try again in %d min(s) (retry #%d)", server.Config.Interval, retryCount)
				}
			} else if errors.Is(err, internal.ErrCloudflareBlocked) {
				if shouldLog {
					ch.Info("channel was blocked by Cloudflare; try with `-cookies` and `-user-agent`? try again in %d min(s) (retry #%d)", server.Config.Interval, retryCount)
				}
			} else if errors.Is(err, context.Canceled) {
				ch.Info("context canceled during retry #%d", retryCount)
			} else {
				ch.Error("on retry #%d: %s: retrying in %d min(s)", retryCount, err.Error(), server.Config.Interval)
			}
		}
		
		err = retry.Do(
			pipeline,
			retry.Context(ctx),
			retry.Attempts(0),
			retry.Delay(time.Duration(server.Config.Interval)*time.Minute),
			retry.DelayType(retry.FixedDelay),
			retry.OnRetry(onRetry),
		)
		
		if err != nil {
			ch.Error("retry loop exited with error: %s", err.Error())
			break
		} else {
			// Reset retry count on successful connection
			retryCount = 0
			ch.Info("stream recording completed successfully, restarting monitoring...")
		}
	}

	// Always cleanup when monitor exits, regardless of error
	if err := ch.Cleanup(); err != nil {
		ch.Error("cleanup on monitor exit: %s", err.Error())
	}

	// Mark monitor as inactive
	ch.MonitorActive = false

	// Enhanced exit logging
	if err != nil && !errors.Is(err, context.Canceled) {
		ch.Error("MONITOR EXIT: record stream failed after %d retries: %s", retryCount, err.Error())
	} else if errors.Is(err, context.Canceled) {
		ch.Info("MONITOR EXIT: stopped due to context cancellation (user action)")
	} else {
		ch.Info("MONITOR EXIT: completed normally")
	}
}

// Update sends an update signal to the channel's update channel.
// This notifies the Server-sent Event to boradcast the channel information to the client.
func (ch *Channel) Update() {
	ch.UpdateCh <- true
}

// RecordStream records the stream of the channel using the provided client.
// It retrieves the stream information and starts watching the segments.
func (ch *Channel) RecordStream(ctx context.Context, client *chaturbate.Client) error {
	stream, err := client.GetStream(ctx, ch.Config.Username)
	if err != nil {
		return fmt.Errorf("get stream: %w", err)
	}
	
	ch.StreamedAt = time.Now().Unix()
	ch.Sequence = 0

	if err := ch.NextFile(); err != nil {
		return fmt.Errorf("next file: %w", err)
	}
	ch.Info("started recording to: %s", ch.File.Name())

	// Ensure file is cleaned up when this function exits in any case
	defer func() {
		if err := ch.Cleanup(); err != nil {
			ch.Error("cleanup on record stream exit: %s", err.Error())
		}
	}()

	playlist, err := stream.GetPlaylist(ctx, ch.Config.Resolution, ch.Config.Framerate)
	if err != nil {
		return fmt.Errorf("get playlist: %w", err)
	}
	ch.UpdateOnlineStatus(true) // Update online status after `GetPlaylist` is OK

	ch.Info("stream quality - resolution %dp (target: %dp), framerate %dfps (target: %dfps)", playlist.Resolution, ch.Config.Resolution, playlist.Framerate, ch.Config.Framerate)

	err = playlist.WatchSegments(ctx, ch.HandleSegment)
	if err != nil {
		ch.Info("recording stopped: %v", err)
	} else {
		ch.Info("recording completed normally")
	}
	
	return err
}

// HandleSegment processes and writes segment data to a file.
func (ch *Channel) HandleSegment(b []byte, duration float64) error {
	if ch.Config.IsPaused {
		return retry.Unrecoverable(internal.ErrPaused)
	}

	n, err := ch.File.Write(b)
	if err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	ch.Filesize += n
	ch.Duration += duration
	ch.Info("duration: %s, filesize: %s", internal.FormatDuration(ch.Duration), internal.FormatFilesize(ch.Filesize))

	// Send an SSE update to update the view
	ch.Update()

	if ch.ShouldSwitchFile() {
		if err := ch.NextFile(); err != nil {
			return fmt.Errorf("next file: %w", err)
		}
		ch.Info("max filesize or duration exceeded, new file created: %s", ch.File.Name())
		return nil
	}
	return nil
}
