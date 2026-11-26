package monitor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/BBZero/op-watch-go/config"
	"github.com/BBZero/op-watch-go/logger"
)

const (
	mysqlAliveMonitTag = "MysqlAliveMonit"
	mysqlAliveProcName = "mysqld"
	mysqlAlivePgrepTO  = 10 * time.Second
)

// MysqlAliveMonitor checks the local mysqld process and its listening port.
// An alert is sent only after RecheckCnt consecutive unhealthy checks.
type MysqlAliveMonitor struct {
	cfg        config.MysqlAliveMonitConfig
	mailSender *MailSender
	stopCh     <-chan struct{}
	done       chan struct{}
	logtag     string

	checkCount int
	failCount  int
	alertSent  bool
	recheck    string
	pid        *int
}

func NewMysqlAliveMonitor(mc config.MysqlAliveMonitConfig, mailSender *MailSender, stopCh <-chan struct{}) *MysqlAliveMonitor {
	return &MysqlAliveMonitor{
		cfg:        mc,
		mailSender: mailSender,
		stopCh:     stopCh,
		done:       make(chan struct{}),
		logtag:     "[" + mysqlAliveMonitTag + "]",
	}
}

func (m *MysqlAliveMonitor) ThreadName() string { return mysqlAliveMonitTag }

func (m *MysqlAliveMonitor) Start() { go m.run() }

func (m *MysqlAliveMonitor) Join(timeout time.Duration) bool {
	select {
	case <-m.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (m *MysqlAliveMonitor) run() {
	logger.Infof("%s monitor thread started, interval=%ds, recheck_interval=%ds, recheck_cnt=%d, port=%d",
		m.logtag, m.cfg.Interval, m.cfg.RecheckInterval, m.cfg.RecheckCnt, m.cfg.Port)
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
		wait := m.checkOnce()
		timer := time.NewTimer(wait)
		select {
		case <-m.stopCh:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// findProcess returns (nil, true) when mysqld is not running, ok=false when the scan itself failed.
func (m *MysqlAliveMonitor) findProcess() (pid *int, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), mysqlAlivePgrepTO)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pgrep", "-x", mysqlAliveProcName)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			logger.Errorf("%s process check failed: pgrep timeout", m.logtag)
			return nil, false
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() == 1 {
				return nil, true
			}
			logger.Errorf("%s pgrep failed: code=%d, stderr=%s",
				m.logtag, exitErr.ExitCode(), strings.TrimSpace(stderr.String()))
			return nil, false
		}
		logger.Errorf("%s process check failed: %v", m.logtag, err)
		return nil, false
	}
	fields := strings.Fields(stdout.String())
	if len(fields) == 0 {
		logger.Errorf("%s pgrep returned no PID", m.logtag)
		return nil, false
	}
	p, err := strconv.Atoi(fields[0])
	if err != nil {
		logger.Errorf("%s process check failed: %v", m.logtag, err)
		return nil, false
	}
	return &p, true
}

func (m *MysqlAliveMonitor) isPortListening() (bool, error) {
	checked := false
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		found, err := m.scanListenPort(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, err
		}
		if found {
			return true, nil
		}
		checked = true
	}
	if !checked {
		return false, fmt.Errorf("TCP listening state is unavailable")
	}
	return false, nil
}

func (m *MysqlAliveMonitor) scanListenPort(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Scan() // header
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// state 0A == LISTEN
		if len(fields) < 4 || fields[3] != "0A" {
			continue
		}
		idx := strings.LastIndex(fields[1], ":")
		if idx < 0 {
			return false, fmt.Errorf("unexpected local address %q in %s", fields[1], path)
		}
		port, err := strconv.ParseInt(fields[1][idx+1:], 16, 32)
		if err != nil {
			return false, err
		}
		if int(port) == m.cfg.Port {
			return true, nil
		}
	}
	return false, scanner.Err()
}

func (m *MysqlAliveMonitor) notify(event, message string, pid *int) {
	if m.mailSender == nil {
		return
	}
	m.mailSender.sendProcessNoti(mysqlAliveProcName, event, message,
		flagPrefix+"mysqlalive-"+strconv.Itoa(m.checkCount), pid)
}

func pidStr(pid *int) string {
	if pid == nil {
		return "None"
	}
	return strconv.Itoa(*pid)
}

// checkOnce runs one check and returns how long to wait before the next one.
func (m *MysqlAliveMonitor) checkOnce() (wait time.Duration) {
	interval := time.Duration(m.cfg.Interval) * time.Second
	wait = interval
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("%s unexpected exception in monitor: %v", m.logtag, r)
			wait = interval
		}
	}()

	m.checkCount++
	pid, checkOK := m.findProcess()
	needPrintLog := m.checkCount <= 10 || m.checkCount%20 == 0

	if !checkOK {
		if needPrintLog {
			logger.Warnf("%s check_count=%d mysqld process not found", m.logtag, m.checkCount)
		}
		return interval
	}

	listening, err := m.isPortListening()
	if err != nil {
		logger.Errorf("%s check_count=%d, port check failed: %v", m.logtag, m.checkCount, err)
		return interval
	}

	if pid != nil && listening {
		lastPid := m.pid
		m.pid = pid
		m.failCount = 0
		m.recheck = ""

		if m.alertSent {
			m.alertSent = false
			needPrintLog = false
			logger.Infof("%s check_count=%d mysqld recovered, pid=%d, port=%d",
				m.logtag, m.checkCount, *pid, m.cfg.Port)
			m.notify(processEventStarted,
				fmt.Sprintf("mysqld is running and port %d is listening", m.cfg.Port), pid)
		} else if lastPid != nil && *lastPid != *pid {
			needPrintLog = false
			logger.Warnf("%s check_count=%d mysqld restarted, last_pid=%d, current_pid=%d",
				m.logtag, m.checkCount, *lastPid, *pid)
			m.notify(processEventRestarted,
				fmt.Sprintf("mysqld has restarted with a new pid, last_pid=%d, current_pid=%d", *lastPid, *pid), pid)
		}
		if needPrintLog {
			logger.Infof("%s check count=%d, check_ok=%v, pid=%s, alert_sent=%v, recheck_state=%s",
				m.logtag, m.checkCount, checkOK, pidStr(pid), m.alertSent, m.recheck)
		}
		return interval
	}

	// Already alerted: wait for recovery without further processing.
	if m.alertSent {
		if needPrintLog {
			logger.Infof("%s check count=%d, check_ok=%v, pid=%s, alert_sent=%v, recheck_state=%s",
				m.logtag, m.checkCount, checkOK, pidStr(pid), m.alertSent, m.recheck)
		}
		return interval
	}

	m.failCount++
	m.recheck = "stopped"
	if m.failCount < m.cfg.RecheckCnt {
		logger.Warnf("%s check_count=%d mysqld unhealthy: pid=%s, port_listening=%v, failed_count/recheck_count:%d/%d",
			m.logtag, m.checkCount, pidStr(pid), listening, m.failCount, m.cfg.RecheckCnt)
		return time.Duration(m.cfg.RecheckInterval) * time.Second
	}

	m.alertSent = true
	m.recheck = ""
	logger.Warnf("%s check_count=%d mysqld unavailable after %d checks: pid=%s, port %d listening=%v",
		m.logtag, m.checkCount, m.cfg.RecheckCnt, pidStr(pid), m.cfg.Port, listening)
	m.notify(processEventStopped,
		fmt.Sprintf("mysqld is unavailable: pid=%s, port %d listening=%s. recheckCnt=%d",
			pidStr(pid), m.cfg.Port, pyBool(listening), m.cfg.RecheckCnt), nil)
	return interval
}

func pyBool(b bool) string {
	if b {
		return "True"
	}
	return "False"
}
