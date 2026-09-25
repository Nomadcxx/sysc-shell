package shell

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// ponytail: 64 KiB bounds MIME offers; stream-parse types if a real client exceeds it.
const maxClipboardTypesBytes = 64 << 10

func readSystemClipboard(ctx context.Context) (v1.ClipboardReadResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	types, err := clipboardCommandOutput(ctx, maxClipboardTypesBytes, "--list-types")
	if err != nil {
		return v1.ClipboardReadResult{}, fmt.Errorf("list clipboard types: %w", err)
	}
	mimeType := plainTextClipboardType(types)
	if mimeType == "" {
		return v1.ClipboardReadResult{}, fmt.Errorf("clipboard has no supported plain-text type")
	}

	data, err := clipboardCommandOutput(ctx, v1.MaxInputBytes, "--no-newline", "--type", mimeType)
	if err != nil {
		return v1.ClipboardReadResult{}, fmt.Errorf("read clipboard: %w", err)
	}
	if !utf8.Valid(data) {
		return v1.ClipboardReadResult{}, fmt.Errorf("clipboard does not contain valid UTF-8 text")
	}
	for _, b := range data {
		if b == 0 {
			return v1.ClipboardReadResult{}, fmt.Errorf("clipboard text contains a NUL byte")
		}
	}
	return v1.ClipboardReadResult{Text: string(data)}, nil
}

func clipboardCommandOutput(ctx context.Context, limit int, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "wl-paste", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	data, readErr := io.ReadAll(io.LimitReader(stdout, int64(limit)+1))
	if len(data) > limit || readErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if len(data) > limit {
			return nil, fmt.Errorf("clipboard output exceeds the %d-byte limit", limit)
		}
		return nil, readErr
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return data, nil
}

// Only ask for offered text/plain encodings that our UTF-8 check can safely
// consume; auto-selection could return HTML when an app offers both formats.
func plainTextClipboardType(offered []byte) string {
	selected, selectedRank := "", 3
	for _, raw := range strings.Split(string(offered), "\n") {
		candidate := strings.TrimSpace(raw)
		if candidate == "" {
			continue
		}
		rank := plainTextClipboardRank(candidate)
		if rank >= 0 && rank < selectedRank {
			selected, selectedRank = candidate, rank
		}
	}
	return selected
}

func plainTextClipboardRank(mimeType string) int {
	parts := strings.Split(mimeType, ";")
	if !strings.EqualFold(strings.TrimSpace(parts[0]), "text/plain") {
		return -1
	}
	charset := ""
	for _, raw := range parts[1:] {
		key, value, ok := strings.Cut(raw, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "charset") {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		if charset != "" && !strings.EqualFold(charset, value) {
			return -1
		}
		charset = value
	}
	switch {
	case charset == "" || strings.EqualFold(charset, "utf-8"):
		if charset == "" {
			return 1
		}
		return 0
	case strings.EqualFold(charset, "us-ascii"):
		return 2
	default:
		return -1
	}
}
