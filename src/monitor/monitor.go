// Package monitor implements the process and disk usage monitors.
package monitor

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BBZero/op-watch-go/logger"
)

const (
	processMonitTagPrefix = "ProcessMonit-"
	diskMonitTagPrefix    = "DiskMonit-"
	flagPrefix            = "oppmonit-"
	mailTimeout           = 10 * time.Second

	processEventStarted   = "started"
	processEventStopped   = "stopped"
	processEventRestarted = "restarted"

	diskStateNone    = "none"
	diskStateOK      = "ok"
	diskStateExceed  = "exceed"
	diskStateRecover = "recover"
	diskStateCkfail  = "ckfail"
)

// Monitor is a monitoring goroutine that can be started and joined.
type Monitor interface {
	ThreadName() string
	Start()
	// Join waits for the monitor goroutine to finish; returns false on timeout.
	Join(timeout time.Duration) bool
}

// GetMachineUptime returns the machine uptime in seconds, 0 on failure.
func GetMachineUptime() float64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		logger.Errorf("Failed to get machine uptime: %v", err)
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		logger.Errorf("Failed to get machine uptime: %v", fmt.Errorf("empty /proc/uptime content"))
		return 0
	}
	uptime, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		logger.Errorf("Failed to get machine uptime: %v", err)
		return 0
	}
	return uptime
}
