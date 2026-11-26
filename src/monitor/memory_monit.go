package monitor

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/mem"

	"github.com/BBZero/op-watch-go/config"
	"github.com/BBZero/op-watch-go/logger"
)

const (
	memoryMonitTag = "MemoryMonit"

	memStateNormal  = "normal"
	memStateWarning = "exceeded"
	memStateCkfail  = "ckfail"

	notiLevelInfo    = "info"
	notiLevelWarning = "warn"
	notiLevelError   = "error"

	procMeminfo = "/proc/meminfo"
)

// MemoryUsageMonitor monitors physical memory usage via gopsutil.
// A state change is only confirmed after RecheckCnt consecutive checks.
type MemoryUsageMonitor struct {
	cfg        config.MemMonitConfig
	mailSender *MailSender
	stopCh     <-chan struct{}
	done       chan struct{}
	logtag     string

	checkCount     int
	currentState   string
	candidateState string
	candidateCount int
	ckfailActive   bool
}

func NewMemoryUsageMonitor(mc config.MemMonitConfig, mailSender *MailSender, stopCh <-chan struct{}) *MemoryUsageMonitor {
	return &MemoryUsageMonitor{
		cfg:          mc,
		mailSender:   mailSender,
		stopCh:       stopCh,
		done:         make(chan struct{}),
		logtag:       "[" + memoryMonitTag + "]",
		currentState: memStateNormal,
	}
}

func (m *MemoryUsageMonitor) ThreadName() string { return memoryMonitTag }

func (m *MemoryUsageMonitor) Start() { go m.run() }

func (m *MemoryUsageMonitor) Join(timeout time.Duration) bool {
	select {
	case <-m.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (m *MemoryUsageMonitor) run() {
	logger.Infof("%s monitor thread started, interval=%ds, recheck_interval=%ds, recheck_cnt=%d",
		m.logtag, m.cfg.Interval, m.cfg.RecheckInterval, m.cfg.RecheckCnt)
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

// getMemoryUsage returns total, used, available bytes and usage percent.
// used = Total - Available (memory that cannot be reclaimed easily).
func getMemoryUsage() (total, used, available int64, usagePct float64, err error) {
	vm, err := mem.VirtualMemory()
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("failed to read memory info: %v", err)
	}
	if vm.Total > uint64(math.MaxInt64) || vm.Available > uint64(math.MaxInt64) {
		return 0, 0, 0, 0, fmt.Errorf("invalid Total or Available in memory info")
	}
	total = int64(vm.Total)
	available = int64(vm.Available)
	if total <= 0 || available > total {
		return 0, 0, 0, 0, fmt.Errorf("invalid Total or Available in memory info")
	}
	used = total - available
	usagePct = float64(used) / float64(total) * 100.0
	return total, used, available, usagePct, nil
}

/* old implementation, reads /proc/meminfo directly
func getMemoryUsageProc() (total, used, available int64, usagePct float64, err error) {
	f, err := os.Open(procMeminfo)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("failed to read %s: %v", procMeminfo, err)
	}
	defer f.Close()

	values := make(map[string]int64)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, rest, ok := strings.Cut(scanner.Text(), ":")
		if !ok || (key != "MemTotal" && key != "MemAvailable") {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return 0, 0, 0, 0, fmt.Errorf("failed to read %s: empty value for %s", procMeminfo, key)
		}
		kb, perr := strconv.ParseInt(fields[0], 10, 64)
		if perr != nil {
			return 0, 0, 0, 0, fmt.Errorf("failed to read %s: %v", procMeminfo, perr)
		}
		values[key] = kb * 1024
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("failed to read %s: %v", procMeminfo, err)
	}

	var missing []string
	for _, k := range []string{"MemTotal", "MemAvailable"} {
		if _, ok := values[k]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return 0, 0, 0, 0, fmt.Errorf("missing %s in %s", strings.Join(missing, "/"), procMeminfo)
	}

	total = values["MemTotal"]
	available = values["MemAvailable"]
	if total <= 0 || available < 0 || available > total {
		return 0, 0, 0, 0, fmt.Errorf("invalid MemTotal or MemAvailable in %s", procMeminfo)
	}
	used = total - available
	usagePct = float64(used) / float64(total) * 100.0
	return total, used, available, usagePct, nil
}
*/

// targetState returns the state to switch to, "" if no change is wanted.
func (m *MemoryUsageMonitor) targetState(usagePct float64, available int64) string {
	hitWarning := usagePct >= m.cfg.WarnThreshold || available <= m.cfg.WarnAvailableBytes
	switch m.currentState {
	case memStateNormal:
		if hitWarning {
			return memStateWarning
		}
	case memStateWarning:
		if usagePct <= m.cfg.RecoverThreshold && available > m.cfg.WarnAvailableBytes {
			return memStateNormal
		}
	}
	return ""
}

func (m *MemoryUsageMonitor) notify(state string, total, used, available *int64,
	usagePct, threshold *float64, message string) {
	if m.mailSender == nil {
		return
	}
	thresholdStr, usedPctStr := "", ""
	if threshold != nil {
		thresholdStr = formatPercent(*threshold) + "%"
	}
	if usagePct != nil {
		usedPctStr = formatPercent(*usagePct) + "%"
	}

	var level, subject, title string
	switch state {
	case memStateWarning:
		level = notiLevelWarning
		subject = "記憶體使用超限"
		title = fmt.Sprintf("記憶體使用超過警告條件. 當前已用: %s", usedPctStr)
	case memStateCkfail:
		level = notiLevelError
		subject = "記憶體檢測失敗"
		title = "記憶體檢測失敗"
	default:
		level = notiLevelInfo
		subject = "記憶體使用恢復"
		title = fmt.Sprintf("記憶體使用恢復. 當前已用: %s", usedPctStr)
	}

	var rows [][2]string
	if usagePct != nil {
		rows = append(rows, [2]string{"使用百分比", usedPctStr})
	}
	if used != nil {
		rows = append(rows, [2]string{"已用", formatBytes(*used)})
	}
	if total != nil {
		rows = append(rows, [2]string{"總共", formatBytes(*total)})
	}
	if available != nil {
		rows = append(rows, [2]string{"可用", formatBytes(*available)})
	}
	if thresholdStr != "" {
		if state == memStateWarning {
			rows = append(rows, [2]string{"警告閾值", thresholdStr})
		} else if state == memStateNormal {
			rows = append(rows, [2]string{"恢復閾值", thresholdStr})
		}
	}
	rows = append(rows, [2]string{"纍計檢測次數", strconv.Itoa(m.checkCount)})

	m.mailSender.sendNormalNoti(level, subject, title, message,
		flagPrefix+"-mem-"+strconv.Itoa(m.checkCount), m.logtag, rows)
}

// checkOnce runs one check and returns how long to wait before the next one.
func (m *MemoryUsageMonitor) checkOnce() (wait time.Duration) {
	interval := time.Duration(m.cfg.Interval) * time.Second
	wait = interval
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("%s unexpected exception in monitor: %v", m.logtag, r)
			wait = interval
		}
	}()

	m.checkCount++
	total, used, available, usagePct, err := getMemoryUsage()
	return m.handleMemoryResult(total, used, available, usagePct, err)
}

func (m *MemoryUsageMonitor) handleMemoryResult(total, used, available int64, usagePct float64, err error) time.Duration {
	interval := time.Duration(m.cfg.Interval) * time.Second
	if err != nil {
		m.candidateState = ""
		m.candidateCount = 0
		if !m.ckfailActive {
			logger.Errorf("%s check failed: %v, check_count=%d", m.logtag, err, m.checkCount)
			m.notify(memStateCkfail, nil, nil, nil, nil, nil, fmt.Sprintf("記憶體檢測失敗: %v.", err))
			m.ckfailActive = true
		}
		return interval
	}

	if m.ckfailActive {
		logger.Infof("%s memory check recovered, check_count=%d", m.logtag, m.checkCount)
		m.ckfailActive = false
	}

	target := m.targetState(usagePct, available)
	if target == "" {
		if m.candidateState != "" {
			logger.Infof("%s state transition cancelled: candidate=%s, usage=%.2f%%",
				m.logtag, m.candidateState, usagePct)
		}
		m.candidateState = ""
		m.candidateCount = 0
		if m.checkCount <= 10 || m.checkCount%100 == 0 {
			logger.Infof("%s check res: state=%s, usage=%.2f%%, total=%d, used=%d, available=%d, check_count=%d",
				m.logtag, m.currentState, usagePct, total, used, available, m.checkCount)
		}
		return interval
	}

	if target != m.candidateState {
		m.candidateState = target
		m.candidateCount = 1
	} else {
		m.candidateCount++
	}

	threshold := m.cfg.RecoverThreshold
	if target == memStateWarning {
		threshold = m.cfg.WarnThreshold
	}
	logger.Infof("%s recheck: current=%s, candidate=%s, count=%d/%d, usage=%.2f%%, total=%d, used=%d, available=%d",
		m.logtag, m.currentState, target, m.candidateCount, m.cfg.RecheckCnt, usagePct, total, used, available)

	if m.candidateCount < m.cfg.RecheckCnt {
		return time.Duration(m.cfg.RecheckInterval) * time.Second
	}

	oldState := m.currentState
	m.currentState = target
	m.candidateState = ""
	m.candidateCount = 0
	message := fmt.Sprintf("記憶體使用狀態改變: %s -> %s", oldState, target)
	logger.Warnf("%s %s, usage=%.2f%%, total=%d, used=%d, avail=%d",
		m.logtag, message, usagePct, total, used, available)
	m.notify(target, &total, &used, &available, &usagePct, &threshold, message)
	return interval
}

// formatBytes formats like python format_bytes: one decimal, trailing zeros trimmed.
func formatBytes(size int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	value := float64(size)
	for i, unit := range units {
		if (value < 1024 && value > -1024) || i == len(units)-1 {
			s := strconv.FormatFloat(value, 'f', 1, 64)
			s = strings.TrimRight(s, "0")
			s = strings.TrimRight(s, ".")
			return s + " " + unit
		}
		value /= 1024
	}
	return ""
}
