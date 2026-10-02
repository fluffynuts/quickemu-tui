package qemu

import (
	"errors"
	"io"
	"net"
	"regexp"
	"strings"
	"time"
)

// MonitorError means the QEMU monitor didn't answer, or answered with an error.
type MonitorError struct {
	Msg string
}

func (e *MonitorError) Error() string {
	return "qemu monitor: " + e.Msg
}

var (
	// ErrMonitorTimeout: nothing answered in time; usually another client
	// (e.g. a socat session) is attached, since the monitor serves one at a time.
	ErrMonitorTimeout = &MonitorError{Msg: "timed out waiting for the monitor prompt (is another client attached?)"}
	// ErrMonitorClosed: qemu hung up, which is expected after "quit".
	ErrMonitorClosed = &MonitorError{Msg: "the monitor closed the connection"}
)

const prompt = "(qemu) "

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func cleanTerminal(raw []byte) string {
	return strings.ReplaceAll(ansiRe.ReplaceAllString(string(raw), ""), "\r", "")
}

func readUntilPrompt(conn net.Conn, deadline time.Time) ([]byte, error) {
	var buf []byte
	chunk := make([]byte, 4096)
	for !strings.HasSuffix(cleanTerminal(buf), prompt) {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
		n, err := conn.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if err == nil {
			continue
		}
		if strings.HasSuffix(cleanTerminal(buf), prompt) {
			break
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil, ErrMonitorTimeout
		}
		if errors.Is(err, io.EOF) {
			return nil, ErrMonitorClosed
		}
		return nil, err
	}
	return buf, nil
}

// CleanMonitorOutput strips terminal escapes, the trailing prompt and the line
// where the monitor's readline echoes the command back.
func CleanMonitorOutput(raw []byte, command string) string {
	text := strings.TrimSuffix(cleanTerminal(raw), prompt)
	lines := strings.Split(text, "\n")
	fields := strings.Fields(command)
	if len(lines) > 0 && len(fields) > 0 && strings.Contains(lines[0], fields[0]) {
		lines = lines[1:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// MonitorCommand runs one HMP command over quickemu's monitor socket
// (qemu -monitor unix:...,server,nowait) and returns its output.
func MonitorCommand(socketPath, command string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	conn, err := net.DialTimeout("unix", socketPath, timeout)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	if _, err := readUntilPrompt(conn, deadline); err != nil {
		return "", err
	}
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return "", err
	}
	if _, err := conn.Write([]byte(command + "\n")); err != nil {
		return "", err
	}
	raw, err := readUntilPrompt(conn, deadline)
	if err != nil {
		return "", err
	}
	return CleanMonitorOutput(raw, command), nil
}
