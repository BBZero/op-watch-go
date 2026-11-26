package monitor

import (
	"reflect"
	"testing"
)

func TestDiskMonitorTransitions(t *testing.T) {
	tests := []struct {
		name   string
		cycle  int
		states []string
		events []string
	}{
		{"cycle and recovery", 2, []string{diskStateExceed, diskStateExceed, diskStateRecover}, []string{diskStateExceed, diskStateRecover}},
		{"hysteresis avoids repeat alert", 1, []string{diskStateExceed, diskStateOK, diskStateExceed, diskStateRecover}, []string{diskStateExceed, diskStateRecover}},
		{"failure and recovery after alert", 1, []string{diskStateExceed, diskStateCkfail, diskStateCkfail, diskStateExceed, diskStateRecover}, []string{diskStateExceed, diskStateCkfail, diskStateRecover}},
		{"failure then recovery without alert", 1, []string{diskStateCkfail, diskStateRecover}, []string{diskStateCkfail}},
		{"interrupted exceed cycle", 2, []string{diskStateExceed, diskStateOK, diskStateExceed}, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			monitor := &DiskUsageMonitor{cycle: test.cycle, currentState: diskStateNone}
			var events []string
			notify := func(state, _ string, _, _, _, _ *int64, _, _ *float64) {
				events = append(events, state)
			}
			for _, state := range test.states {
				monitor.checkCount++
				monitor.handleDiskResult(state, nil, nil, nil, nil, nil, "test", notify)
			}
			if !reflect.DeepEqual(events, test.events) {
				t.Errorf("events = %v, want %v", events, test.events)
			}
		})
	}
}
