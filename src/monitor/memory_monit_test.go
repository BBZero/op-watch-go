package monitor

import (
	"errors"
	"testing"
	"time"

	"github.com/BBZero/op-watch-go/config"
)

func TestMemoryTargetState(t *testing.T) {
	monitor := &MemoryUsageMonitor{cfg: config.MemMonitConfig{
		WarnThreshold: 80, RecoverThreshold: 75, WarnAvailableBytes: 256,
	}}
	tests := []struct {
		name      string
		state     string
		usage     float64
		available int64
		want      string
	}{
		{"normal below warning", memStateNormal, 79, 257, ""},
		{"warning threshold inclusive", memStateNormal, 80, 257, memStateWarning},
		{"available threshold inclusive", memStateNormal, 50, 256, memStateWarning},
		{"hysteresis retains warning", memStateWarning, 76, 257, ""},
		{"recover threshold inclusive", memStateWarning, 75, 257, memStateNormal},
		{"low available prevents recovery", memStateWarning, 75, 256, ""},
		{"both recovery conditions met", memStateWarning, 50, 257, memStateNormal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			monitor.currentState = test.state
			if got := monitor.targetState(test.usage, test.available); got != test.want {
				t.Errorf("targetState(%v, %d) = %q, want %q", test.usage, test.available, got, test.want)
			}
		})
	}
}

func TestMemoryRecheckTransitions(t *testing.T) {
	monitor := &MemoryUsageMonitor{
		cfg: config.MemMonitConfig{
			WarnThreshold: 80, RecoverThreshold: 75, WarnAvailableBytes: 256,
			Interval: 60, RecheckInterval: 10, RecheckCnt: 3,
		},
		currentState: memStateNormal,
	}
	checks := []struct {
		name      string
		usage     float64
		available int64
		err       error
		wait      time.Duration
		state     string
		candidate string
		count     int
	}{
		{"warning first check", 80, 300, nil, 10 * time.Second, memStateNormal, memStateWarning, 1},
		{"warning cancelled", 79, 300, nil, 60 * time.Second, memStateNormal, "", 0},
		{"warning first recheck", 80, 300, nil, 10 * time.Second, memStateNormal, memStateWarning, 1},
		{"warning second recheck", 80, 300, nil, 10 * time.Second, memStateNormal, memStateWarning, 2},
		{"warning confirmed", 80, 300, nil, 60 * time.Second, memStateWarning, "", 0},
		{"hysteresis", 76, 300, nil, 60 * time.Second, memStateWarning, "", 0},
		{"recovery first recheck", 75, 300, nil, 10 * time.Second, memStateWarning, memStateNormal, 1},
		{"failed check cancels candidate", 0, 0, errors.New("test"), 60 * time.Second, memStateWarning, "", 0},
		{"recovery restarts", 75, 300, nil, 10 * time.Second, memStateWarning, memStateNormal, 1},
		{"recovery second recheck", 75, 300, nil, 10 * time.Second, memStateWarning, memStateNormal, 2},
		{"recovery confirmed", 75, 300, nil, 60 * time.Second, memStateNormal, "", 0},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			monitor.checkCount++
			wait := monitor.handleMemoryResult(1000, 800, check.available, check.usage, check.err)
			if wait != check.wait || monitor.currentState != check.state || monitor.candidateState != check.candidate || monitor.candidateCount != check.count {
				t.Errorf("got wait=%v state=%q candidate=%q count=%d, want wait=%v state=%q candidate=%q count=%d",
					wait, monitor.currentState, monitor.candidateState, monitor.candidateCount,
					check.wait, check.state, check.candidate, check.count)
			}
		})
	}
}
