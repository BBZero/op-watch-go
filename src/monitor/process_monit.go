package monitor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/BBZero/op-watch-go/config"
	"github.com/BBZero/op-watch-go/logger"
)

// ProcessMonitor checks one process pattern periodically and notifies on
// stop/restart/recover events. Each monitor runs in its own goroutine.
type ProcessMonitor struct {
	name         string
	pattern      string
	interval     time.Duration
	cycle        int
	checkTimeout time.Duration
	mailSender   *MailSender
	stopCh       <-chan struct{}
	done         chan struct{}
	logtag       string

	checkCount    int
	failCount     int
	pid           *int
	failAlertSent bool
}

func NewProcessMonitor(pc config.ProcessMonitConfig, ckTimeout int, mailSender *MailSender, stopCh <-chan struct{}) *ProcessMonitor {
	return &ProcessMonitor{
		name:         pc.Name,
		pattern:      pc.Pattern,
		interval:     time.Duration(pc.Interval) * time.Second,
		cycle:        pc.FailCycle,
		checkTimeout: time.Duration(ckTimeout) * time.Second,
		mailSender:   mailSender,
		stopCh:       stopCh,
		done:         make(chan struct{}),
		logtag:       "[" + processMonitTagPrefix + pc.Name + "]",
	}
}

func (m *ProcessMonitor) ThreadName() string { return processMonitTagPrefix + m.name }

func (m *ProcessMonitor) Start() { go m.run() }

func (m *ProcessMonitor) Join(timeout time.Duration) bool {
	select {
	case <-m.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (m *ProcessMonitor) run() {
	logger.Infof("%s monitor thread started, pattern=%s, interval=%ds, cycle=%d",
		m.logtag, m.pattern, int(m.interval.Seconds()), m.cycle)
	defer func() {
		logger.Infof("%s monitor thread stopped", m.logtag)
		close(m.done)
	}()
	for {
		select {
		case <-m.stopCh:
			return
		default:
		}
		m.checkProcess()
		timer := time.NewTimer(m.interval)
		select {
		case <-m.stopCh:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (m *ProcessMonitor) findProcess() (*int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), m.checkTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pgrep", "-f", m.pattern)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			logger.Errorf("%s pgrep timeout", m.logtag)
			return nil, fmt.Errorf("pgrep timeout")
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() == 1 {
				return nil, nil
			}
			logger.Errorf("%s pgrep failed, pattern=%s, return code=%d, stderr=%s",
				m.logtag, m.pattern, exitErr.ExitCode(), strings.TrimSpace(stderr.String()))
			return nil, fmt.Errorf("pgrep failed with exit code %d", exitErr.ExitCode())
		}
		logger.Errorf("%s process scan failed: %v", m.logtag, err)
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) > 0 && lines[0] != "" {
		pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
		if err != nil {
			logger.Errorf("%s process scan failed: %v", m.logtag, err)
			return nil, err
		}
		return &pid, nil
	}
	return nil, fmt.Errorf("pgrep returned success but no PID")
}

func (m *ProcessMonitor) notify(event string, ckcount int, message string, pid *int) {
	if m.mailSender == nil {
		return
	}
	m.mailSender.sendProcessNoti(m.name, event, message, flagPrefix+"p-"+strconv.Itoa(ckcount), pid)
}

func (m *ProcessMonitor) checkProcess() {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("%s unexpected exception in monitor: %v", m.logtag, r)
		}
	}()
	m.checkCount++
	pid, err := m.findProcess()
	m.handleProcessResult(pid, err, m.notify)
}

func (m *ProcessMonitor) handleProcessResult(pid *int, err error, notify func(string, int, string, *int)) {
	if err != nil {
		logger.Errorf("%s process scan failed: %v", m.logtag, err)
		return
	}

	if pid != nil {
		previousFailCount := m.failCount
		previousFailAlertSent := m.failAlertSent
		m.failCount = 0
		oldPid := m.pid
		m.pid = pid
		m.failAlertSent = false
		switch {
		case m.checkCount == 1:
			logger.Infof("%s check res: running, pid=%d, check_count=%d", m.logtag, *pid, m.checkCount)
		case previousFailCount > 0 && previousFailAlertSent:
			logger.Infof("%s check res: recovered, pid=%d, check_count=%d, previous fail count=%d",
				m.logtag, *pid, m.checkCount, previousFailCount)
			message := fmt.Sprintf("Previous Failcnt: %d, Check Count: %d, Pattern: %s",
				previousFailCount, m.checkCount, m.pattern)
			notify(processEventStarted, m.checkCount, message, pid)
		case oldPid != nil && *oldPid > 0 && *pid != *oldPid:
			oldPidStr := strconv.Itoa(*oldPid)
			logger.Warnf("%s check res: pid changed: %s -> %d, check_count=%d",
				m.logtag, oldPidStr, *pid, m.checkCount)
			message := fmt.Sprintf("Pid Changed: %s->%d, Check Count: %d, Pattern: %s",
				oldPidStr, *pid, m.checkCount, m.pattern)
			notify(processEventRestarted, m.checkCount, message, pid)
		case previousFailCount > 0:
			logger.Infof("%s check res: recovered, pid=%d, check_count=%d, previous fail count=%d",
				m.logtag, *pid, m.checkCount, previousFailCount)
			if oldPid != nil && *oldPid > 0 {
				message := fmt.Sprintf("Pid Changed: %d->%d, Previous Failcnt: %d, Check Count: %d, Pattern: %s",
					*oldPid, *pid, previousFailCount, m.checkCount, m.pattern)
				notify(processEventRestarted, m.checkCount, message, pid)
			}
		default:
			if m.checkCount <= 20 || m.checkCount%100 == 0 {
				logger.Infof("%s check res: running, pid=%d, check_count=%d", m.logtag, *pid, m.checkCount)
			}
		}
		return
	}
	// m.pid = nil

	m.failCount++
	hasPrintLog := false
	if m.failCount >= m.cycle && !m.failAlertSent {
		logger.Errorf("%s check res: failed, will alert, check_count=%d, pattern=%s, fail_count=%d",
			m.logtag, m.checkCount, m.pattern, m.failCount)
		hasPrintLog = true
		m.failAlertSent = true
		message := fmt.Sprintf("Fail Count: %d, Check Count: %d, Pattern: %s",
			m.failCount, m.checkCount, m.pattern)
		notify(processEventStopped, m.checkCount, message, nil)
	}
	if !hasPrintLog && (m.failCount == 1 || m.checkCount%100 == 0 || m.checkCount <= 10) {
		logger.Warnf("%s check res: failed, check_count=%d, fail_count=%d", m.logtag, m.checkCount, m.failCount)
	}
}
