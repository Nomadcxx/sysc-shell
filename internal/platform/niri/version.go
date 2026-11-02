package niri

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
)

// Version reads the running compositor's version through its IPC socket.
func Version(ctx context.Context, socketPath string) (string, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if _, err := fmt.Fprintln(conn, `"Version"`); err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 4096), maxLine)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("compositor: no version reply")
	}
	var reply struct {
		Ok  *struct{ Version string }
		Err *string
	}
	if err := json.Unmarshal(scanner.Bytes(), &reply); err != nil {
		return "", err
	}
	if reply.Err != nil {
		return "", fmt.Errorf("compositor version: %s", *reply.Err)
	}
	if reply.Ok == nil || reply.Ok.Version == "" {
		return "", fmt.Errorf("compositor: missing version")
	}
	return reply.Ok.Version, nil
}
