package monitor

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/BBZero/op-watch-go/config"
	"github.com/BBZero/op-watch-go/logger"
)

// DiskUsageMonitor checks the usage of one path/block device periodically and
// notifies on exceed/recover/check-fail events. Each monitor runs in its own
// goroutine.
type DiskUsageMonitor struct {
	name       string
	cfg        config.DiskMonitConfig
	interval   time.Duration
	cycle      int
	mailSender *MailSender
	stopCh     <-chan struct{}
	done       chan struct{}
	logtag     string

	checkCount    int
	currentState  string
	exceedCount   int
	hasNotiExceed bool

	checkTimeout time.Duration // currently unused, same as python
}

func NewDiskUsageMonitor(dc config.DiskMonitConfig, ckTimeout int, mailSender *MailSender, stopCh <-chan struct{}) *DiskUsageMonitor {
	return &DiskUsageMonitor{
		name:         dc.Name,
		cfg:          dc,
		interval:     time.Duration(dc.Interval) * time.Second,
		cycle:        dc.FailCycle,
		mailSender:   mailSender,
		stopCh:       stopCh,
		done:         make(chan struct{}),
		logtag:       "[" + diskMonitTagPrefix + dc.Name + "]",
		currentState: diskStateNone,
		checkTimeout: time.Duration(ckTimeout) * time.Second,
	}
}

func (d *DiskUsageMonitor) ThreadName() string { return diskMonitTagPrefix + d.name }

func (d *DiskUsageMonitor) Start() { go d.run() }

func (d *DiskUsageMonitor) Join(timeout time.Duration) bool {
	select {
	case <-d.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (d *DiskUsageMonitor) run() {
	logger.Infof("%s monitor thread started, path=%s, warn=%.2f%%, recover=%.2f%%, interval=%ds",
		d.logtag, d.cfg.Path, d.cfg.ExceedThreshold, d.cfg.RecoverThreshold, int(d.interval.Seconds()))
	defer func() {
		logger.Infof("%s monitor thread stopped", d.logtag)
		close(d.done)
	}()
	for {
		select {
		case <-d.stopCh:
			return
		default:
		}
		d.checkOnce()
		timer := time.NewTimer(d.interval)
		select {
		case <-d.stopCh:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// resolveMountPoint returns the mount point of a block device, "" if not
// mounted or any stat failed (same as python returning None).
func resolveMountPoint(devPath string) string {
	st, err := os.Stat(devPath)
	if err != nil {
		return ""
	}
	stSys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	rdev := stSys.Rdev

	f, err := os.Open("/proc/mounts")
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "/dev/") {
			continue
		}
		mst, err := os.Stat(fields[0])
		if err != nil {
			continue
		}
		if !isBlockDevice(mst) {
			continue
		}
		msys, ok := mst.Sys().(*syscall.Stat_t)
		if !ok {
			continue
		}
		// Compare device numbers so /dev/disk/by-uuid, /dev/mapper/* symlinks also match.
		if msys.Rdev == rdev {
			return strings.ReplaceAll(fields[1], "\\040", " ")
		}
	}
	if scanner.Err() != nil {
		return ""
	}
	return ""
}

func isBlockDevice(st os.FileInfo) bool {
	mode := st.Mode()
	return mode&os.ModeDevice != 0 && mode&os.ModeCharDevice == 0
}

// checkDiskUsage returns the disk state plus byte counts and usage percent.
// Same math as python shutil.disk_usage / df:
//
//	total = f_blocks * f_frsize
//	used  = (f_blocks - f_bfree) * f_frsize (includes reserved blocks)
//	avail = f_bavail * f_frsize
//	usage_pct = used / (used + avail) * 100
func (d *DiskUsageMonitor) checkDiskUsage() (state string, totalBytes, usedBytes, freeBytes, availBytes *int64, usagePct *float64, reason string) {
	path := d.cfg.Path
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return diskStateCkfail, nil, nil, nil, nil, nil, "Path does not exist"
		}
		return diskStateCkfail, nil, nil, nil, nil, nil, fmt.Sprintf("stat failed: %v", err)
	}
	if isBlockDevice(st) {
		mountPoint := resolveMountPoint(path)
		if mountPoint == "" {
			return diskStateCkfail, nil, nil, nil, nil, nil, "Block device is not mounted or dev stat failed"
		}
		path = mountPoint
	}

	var stfs syscall.Statfs_t
	if err := syscall.Statfs(path, &stfs); err != nil {
		return diskStateCkfail, nil, nil, nil, nil, nil, fmt.Sprintf("Failed to get disk usage: %v", err)
	}
	total := int64(stfs.Blocks) * int64(stfs.Frsize)
	used := (int64(stfs.Blocks) - int64(stfs.Bfree)) * int64(stfs.Frsize)
	avail := int64(stfs.Bavail) * int64(stfs.Frsize)
	free := total - used

	pct := 0.0
	denom := used + avail
	if denom > 0 {
		pct = float64(used) / float64(denom) * 100
	}

	if pct >= d.cfg.ExceedThreshold {
		return diskStateExceed, &total, &used, &free, &avail, &pct, ""
	}
	if pct < d.cfg.RecoverThreshold {
		return diskStateRecover, &total, &used, &free, &avail, &pct, ""
	}
	return diskStateOK, &total, &used, &free, &avail, &pct, ""
}

func (d *DiskUsageMonitor) notify(state, message string,
	usedBytes, totalBytes, freeBytes, availBytes *int64,
	usedPercent, thresholdPercent *float64) {
	if d.mailSender == nil {
		return
	}
	d.mailSender.sendDiskNoti(
		d.name, state, message, d.cfg.Path,
		flagPrefix+"d-"+strconv.Itoa(d.checkCount),
		usedBytes, totalBytes, freeBytes, availBytes,
		usedPercent, thresholdPercent,
	)
}

func (d *DiskUsageMonitor) checkOnce() {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("%s unexpected exception in monitor: %v", d.logtag, r)
		}
	}()
	d.checkCount++
	newState, totalBytes, usedBytes, freeBytes, availBytes, usagePct, reason := d.checkDiskUsage()
	d.handleDiskResult(newState, totalBytes, usedBytes, freeBytes, availBytes, usagePct, reason, d.notify)
}

func (d *DiskUsageMonitor) handleDiskResult(newState string, totalBytes, usedBytes, freeBytes, availBytes *int64,
	usagePct *float64, reason string, notify func(string, string, *int64, *int64, *int64, *int64, *float64, *float64)) {
	shouldNotify := false
	notiState := ""
	message := ""
	oldHasNotiExceed := d.hasNotiExceed

	switch newState {
	case diskStateExceed:
		d.exceedCount++
	case diskStateOK:
		// After an alert, usage in the hysteresis zone (recover <= usage <
		// warning) keeps exceedCount > cycle so a new spike does not send a
		// second exceed alert before a recover is seen.
		if !d.hasNotiExceed {
			d.exceedCount = 0
		}
	default:
		d.exceedCount = 0
	}

	switch {
	case newState == diskStateCkfail && d.currentState != diskStateCkfail:
		shouldNotify = true
		notiState = diskStateCkfail
		message = fmt.Sprintf("Disk check failed: %s. Check Count: %d", reason, d.checkCount)
	case newState == diskStateExceed && d.exceedCount == d.cycle && !d.hasNotiExceed:
		//不加not self.has_notiExceed 的話: EXCEED->中間->exceed->ckfail->exceed(這裏會發), 但是這裏是取捨問題
		shouldNotify = true
		notiState = diskStateExceed
		d.hasNotiExceed = true
		message = fmt.Sprintf("Exceed Count = %d. Check Count: %d", d.exceedCount, d.checkCount)
	case newState == diskStateRecover && d.currentState != diskStateRecover && d.hasNotiExceed:
		shouldNotify = true
		d.hasNotiExceed = false
		notiState = diskStateRecover
		message = fmt.Sprintf("恢復閾值: %.2f%%. Check Count: %d", d.cfg.RecoverThreshold, d.checkCount)
	}

	if totalBytes != nil {
		if d.checkCount <= 10 || d.checkCount%100 == 0 || shouldNotify {
			logger.Infof("%s check res: state=%s, check_count=%d, used=%d, total=%d, free=%d, avail=%d, usage_pct=%.4f%%, "+
				"thresh=%.2f%%, recover_th=%.2f%%, path=%s, exceed_count=%d, old_has_notiExceed=%v",
				d.logtag, newState, d.checkCount, *usedBytes, *totalBytes, *freeBytes, *availBytes, *usagePct,
				d.cfg.ExceedThreshold, d.cfg.RecoverThreshold, d.cfg.Path, d.exceedCount, oldHasNotiExceed)
		}
	} else if d.currentState != diskStateCkfail || d.checkCount <= 10 || shouldNotify {
		logger.Warnf("%s check res: state=%s, check_count=%d, path=%s, reason=%s",
			d.logtag, newState, d.checkCount, d.cfg.Path, reason)
	}

	d.currentState = newState
	if shouldNotify {
		var thresholdPercent *float64
		if notiState != diskStateCkfail {
			thresholdPercent = &d.cfg.ExceedThreshold
		}
		notify(notiState, message,
			usedBytes, totalBytes, freeBytes, availBytes,
			usagePct, thresholdPercent)
	}
}
