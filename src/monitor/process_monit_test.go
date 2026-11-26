package monitor

import (
	"errors"
	"reflect"
	"testing"
)

func TestProcessMonitorEvents(t *testing.T) {
	pid := func(value int) *int { return &value }
	tests := []struct {
		name   string
		cycle  int
		checks []struct {
			pid *int
			err error
		}
		events []string
	}{
		{
			name:  "first running check",
			cycle: 1,
			checks: []struct {
				pid *int
				err error
			}{{pid: pid(10)}},
		},
		{
			name:  "initial failure below cycle then running",
			cycle: 2,
			checks: []struct {
				pid *int
				err error
			}{{}, {pid: pid(10)}},
		},
		{
			name:  "initial stop alert then running",
			cycle: 1,
			checks: []struct {
				pid *int
				err error
			}{{}, {pid: pid(10)}},
			events: []string{processEventStopped, processEventStarted},
		},
		{
			name:  "short failure with same pid",
			cycle: 2,
			checks: []struct {
				pid *int
				err error
			}{{pid: pid(10)}, {}, {pid: pid(10)}},
			events: []string{processEventRestarted},
		},
		{
			name:  "stop alert then changed pid",
			cycle: 1,
			checks: []struct {
				pid *int
				err error
			}{{pid: pid(10)}, {}, {pid: pid(20)}},
			events: []string{processEventStopped, processEventStarted},
		},
		{
			name:  "running pid changed",
			cycle: 1,
			checks: []struct {
				pid *int
				err error
			}{{pid: pid(10)}, {pid: pid(20)}},
			events: []string{processEventRestarted},
		},
		{
			name:  "scan error is not a stop",
			cycle: 1,
			checks: []struct {
				pid *int
				err error
			}{{err: errors.New("scan failed")}, {pid: pid(10)}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			monitor := &ProcessMonitor{cycle: test.cycle}
			var events []string
			notify := func(event string, _ int, _ string, _ *int) {
				events = append(events, event)
			}
			for _, check := range test.checks {
				monitor.checkCount++
				monitor.handleProcessResult(check.pid, check.err, notify)
			}
			if !reflect.DeepEqual(events, test.events) {
				t.Errorf("events = %v, want %v", events, test.events)
			}
		})
	}
}
