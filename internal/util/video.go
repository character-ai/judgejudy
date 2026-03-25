package util

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ExtractFrames extracts evenly-spaced frames from a video file using ffmpeg.
// Returns base64-encoded PNG frames.
func ExtractFrames(videoPath string, numFrames int) ([]string, error) {
	if numFrames <= 0 {
		numFrames = 8
	}

	tmpDir, err := os.MkdirTemp("", "jj_frames_*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Use ffmpeg to extract frames
	outPattern := filepath.Join(tmpDir, "frame_%04d.png")
	cmd := exec.Command("ffmpeg",
		"-i", videoPath,
		"-vf", fmt.Sprintf("select=not(mod(n\\,%d)),setpts=N/TB", numFrames),
		"-frames:v", fmt.Sprintf("%d", numFrames),
		"-vsync", "vfr",
		outPattern,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg frame extraction failed: %w\noutput: %s", err, out)
	}

	// Read extracted frames
	var frames []string
	for i := 1; i <= numFrames; i++ {
		framePath := filepath.Join(tmpDir, fmt.Sprintf("frame_%04d.png", i))
		data, err := os.ReadFile(framePath)
		if err != nil {
			break // No more frames
		}
		frames = append(frames, base64.StdEncoding.EncodeToString(data))
	}

	if len(frames) == 0 {
		return nil, fmt.Errorf("no frames extracted from video")
	}

	return frames, nil
}
