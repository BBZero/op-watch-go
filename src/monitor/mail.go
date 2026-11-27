package monitor

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"unicode"

	"github.com/BBZero/op-watch-go/logger"
)

const (
	mailCaller     = "op-watch"
	mailSenderName = "OpWatch(Go)"
)

// MailSender sends notification emails via an external mail command.
type MailSender struct {
	command    string
	enableNoti bool
}

func NewMailSender(command string, enableNoti bool) *MailSender {
	return &MailSender{command: command, enableNoti: enableNoti}
}

func (m *MailSender) sendNoti(monitorTag string, args []string) {
	logger.Infof("[Mail] %s send: %s", monitorTag, strings.Join(args, " "))
	ctx, cancel := context.WithTimeout(context.Background(), mailTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	exitCode := 0
	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			logger.Errorf("[Mail] %s command timeout after %ds", monitorTag, int(mailTimeout.Seconds()))
			return
		}
		if errors.Is(err, exec.ErrNotFound) {
			logger.Errorf("[Mail] %s command not found: %s", monitorTag, m.command)
			return
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			logger.Errorf("[Mail] %s failed to execute command: %v", monitorTag, err)
			return
		}
	}
	logger.Infof("[Mail] %s command return code=%d", monitorTag, exitCode)
	if out := strings.TrimRightFunc(stdout.String(), unicode.IsSpace); out != "" {
		logger.Infof("[Mail] %s command stdout: %s", monitorTag, out)
	}
	if e := strings.TrimRightFunc(stderr.String(), unicode.IsSpace); e != "" {
		logger.Errorf("[Mail] %s command stderr: %s", monitorTag, e)
	}
}

func (m *MailSender) sendProcessNoti(monitorName, event, message, flag string, pid *int) {
	monitorTag := "[" + processMonitTagPrefix + monitorName + "]"
	if !m.enableNoti {
		logger.Warnf("[Mail] %s sendProcessNoti, but enable_noti false", monitorTag)
		return
	}
	if m.command == "" {
		logger.Warnf("[Mail] %s command is empty, notification skipped", monitorTag)
		return
	}
	args := []string{
		m.command, "send",
		"--type", "process",
		"--name", monitorName,
		"--flag", flag,
		"--event", event,
		"--caller", mailCaller,
		"--sender", mailSenderName,
		"--message", message,
	}
	if pid != nil {
		args = append(args, "--pid", strconv.Itoa(*pid))
	}
	m.sendNoti(monitorTag, args)
}

func (m *MailSender) sendDiskNoti(monitorName, state, message, path, flag string,
	usedBytes, totalBytes, freeBytes, availBytes *int64,
	usedPercent, thresholdPercent *float64) {
	monitorTag := "[" + diskMonitTagPrefix + monitorName + "]"
	if !m.enableNoti {
		logger.Warnf("[Mail] %s sendDiskNoti, but enable_noti false", monitorTag)
		return
	}
	if m.command == "" {
		logger.Warnf("[Mail] %s command is empty, notification skipped", monitorTag)
		return
	}
	args := []string{
		m.command, "send",
		"--type", "disk_usage",
		"--flag", flag,
		"--state", state,
		"--path", path,
		"--caller", mailCaller,
		"--sender", mailSenderName,
		"--message", message,
	}
	if usedPercent != nil {
		args = append(args, "--used-percent", formatPercent(*usedPercent))
	}
	if usedBytes != nil {
		args = append(args, "--used-bytes", strconv.FormatInt(*usedBytes, 10))
	}
	if totalBytes != nil {
		args = append(args, "--total-bytes", strconv.FormatInt(*totalBytes, 10))
	}
	if freeBytes != nil {
		args = append(args, "--free-bytes", strconv.FormatInt(*freeBytes, 10))
	}
	if availBytes != nil {
		args = append(args, "--avail-bytes", strconv.FormatInt(*availBytes, 10))
	}
	if thresholdPercent != nil {
		args = append(args, "--threshold-percent", formatPercent(*thresholdPercent))
	}
	m.sendNoti(monitorTag, args)
}
func (m *MailSender) sendNormalNoti(level, subject, title, message, flag, monitorTag string, rows [][2]string) {
	if !m.enableNoti {
		logger.Warnf("[Mail] %s sendNormalNoti, but enable_noti false", monitorTag)
		return
	}
	if m.command == "" {
		logger.Warnf("[Mail] %s command is empty, notification skipped", monitorTag)
		return
	}
	args := []string{
		m.command, "send",
		"--type", "notify",
		"--subject", subject,
		"--title", title,
		"--flag", flag,
		"--caller", mailCaller,
		"--sender", mailSenderName,
		"--level", level,
		"--message", message,
	}
	for _, r := range rows {
		args = append(args, "--row", r[0]+","+r[1])
	}
	m.sendNoti(monitorTag, args)
}

// formatPercent formats like python f"{pct:.2f}".rstrip("0").rstrip("."):
// two decimals with trailing zeros removed.
func formatPercent(pct float64) string {
	s := strconv.FormatFloat(pct, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}
