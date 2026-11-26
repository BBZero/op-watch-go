package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/BBZero/op-watch-go/config"
	"github.com/BBZero/op-watch-go/logger"
	"github.com/BBZero/op-watch-go/monitor"
	"github.com/BBZero/op-watch-go/version"
)

const (
	mainTag         = "[OpWatch]"
	joinTimeout     = 10 * time.Second
	defaultConfName = "watch.json"
	separator       = "=================================================="
)

var rootCmd = &cobra.Command{
	Use:           "opwatch",
	Short:         "OpWatch is a simple monitor",
	Version:       version.Version,
	SilenceErrors: true,
	SilenceUsage:  true,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version",
	Run: func(cmd *cobra.Command, args []string) {
		// fmt.Println(Version)
		version.PrintVersion()
	},
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the monitor with the config file (default: " + defaultConfName + " in the binary directory, fallback to cwd if executable path is unavailable)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMonitor(runConfPath)
	},
}

var runConfPath string

func init() {
	runCmd.Flags().StringVarP(&runConfPath, "config", "c", defaultConfPath(), "Path to config file")
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.SetVersionTemplate(version.Version + "\n")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%s [ERROR] %v\n", mainTag, err)
		os.Exit(1)
	}
}

func defaultConfPath() string {
	execPath, err := os.Executable()
	if err != nil {
		// If os.Executable() fails, fall back to the relative path watch.json,
		// which is resolved against the current working directory (CWD),
		// not the binary directory.
		return defaultConfName
	}
	return filepath.Join(filepath.Dir(execPath), defaultConfName)
}

func runMonitor(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if err := logger.Init(&logger.Config{
		Path:    cfg.Log.Path,
		Level:   cfg.Log.Level,
		MaxSize: cfg.Log.MaxSize,
		MaxNum:  cfg.Log.MaxNum,
	}); err != nil {
		return fmt.Errorf("Failed to setup logger: %v", err)
	}
	defer logger.Sync()

	logger.Info(separator)
	logger.Info("Main: opwatch starting")
	logger.Infof("Config Path: %s", configPath)
	logger.Infof("Configs: enable=%v machine_updelay=%d enable_noti=%v process_interval=%d process_cycle=%d process_ck_timeout=%d process_monitnum=%d disk_interval=%d disk_cycle=%d disk_ck_timeout=%d disk_monitnum=%d",
		cfg.Enable, cfg.MachineUpdelay, cfg.EnableNotify)
	hasMonitor := false
	if cfg.ProcessMonit != nil {
		logger.Infof("ProcessMonit: interval=%d cycle=%d ck_timeout=%d monitnum=%d",
			cfg.ProcessMonit.Interval, cfg.ProcessMonit.Cycle, cfg.ProcessMonit.CkTimeout, len(cfg.ProcessMonit.Monitors))
		if len(cfg.ProcessMonit.Monitors) > 0 {
			hasMonitor = true
		}
	}
	if cfg.DiskMonit != nil {
		logger.Infof("DiskMonit: interval=%d cycle=%d ck_timeout=%d monitnum=%d",
			cfg.DiskMonit.Interval, cfg.DiskMonit.Cycle, cfg.DiskMonit.CkTimeout, len(cfg.DiskMonit.Monitors))
		if len(cfg.DiskMonit.Monitors) > 0 {
			hasMonitor = true
		}
	}
	if cfg.MemMonit != nil {
		logger.Infof("MemoryMonit: enable=%v interval=%d recheck_interval=%d recheck_cnt=%d warning=%.2f%% recover=%.2f%%",
			cfg.MemMonit.Enable, cfg.MemMonit.Interval, cfg.MemMonit.RecheckInterval,
			cfg.MemMonit.RecheckCnt, cfg.MemMonit.WarnThreshold, cfg.MemMonit.RecoverThreshold)
		if cfg.MemMonit.Enable {
			hasMonitor = true
		}
	}
	if cfg.MysqlAliveMonit != nil {
		logger.Infof("MysqlAliveMonit: enable=%v interval=%d recheck_interval=%d recheck_cnt=%d port=%d",
			cfg.MysqlAliveMonit.Enable, cfg.MysqlAliveMonit.Interval, cfg.MysqlAliveMonit.RecheckInterval,
			cfg.MysqlAliveMonit.RecheckCnt, cfg.MysqlAliveMonit.Port)
		if cfg.MysqlAliveMonit.Enable {
			hasMonitor = true
		}
	}
	if cfg.MysqlReplMonit != nil {
		logger.Infof("MysqlReplMonit: enable=%v interval=%d cycle=%d recheck_delay=%d account=%s ip=%s port=%d",
			cfg.MysqlReplMonit.Enable, cfg.MysqlReplMonit.Interval, cfg.MysqlReplMonit.Cycle, cfg.MysqlReplMonit.RecheckDelay,
			cfg.MysqlReplMonit.Account, cfg.MysqlReplMonit.IP, cfg.MysqlReplMonit.Port)
		if cfg.MysqlReplMonit.Enable {
			hasMonitor = true
		}
	}
	logger.Info(separator)

	if !cfg.Enable {
		logger.Info("Monitor is disabled by configuration. Exit")
		return nil
	}
	if !hasMonitor {
		logger.Warn("No process, disk usage, memory or mysql replication monitors configured. Exit")
		return nil
	}

	stopCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, os.Interrupt)
	go func() {
		sig := <-sigCh
		logger.Infof("Received signal %s, shutting down...", signalName(sig))
		close(stopCh)
	}()

	if cfg.MachineUpdelay > 0 {
		uptime := monitor.GetMachineUptime()
		remaining := float64(cfg.MachineUpdelay) - uptime
		if remaining > 0 {
			logger.Infof("Machine uptime=%.1f seconds, waiting %.1f seconds before monitoring", uptime, remaining)
			select {
			case <-stopCh:
				logger.Info("Shutdown requested during machine_updelay")
				return nil
			case <-time.After(time.Duration(remaining * float64(time.Second))):
			}
		} else {
			logger.Infof("Machine uptime=%.1f seconds, machine_updelay=%d already reached", uptime, cfg.MachineUpdelay)
		}
	}

	var mailSender *monitor.MailSender = nil
	if cfg.EnableNotify {
		mailSender = monitor.NewMailSender(cfg.MailCmd, cfg.EnableNotify)
	}

	var monitors []monitor.Monitor
	if cfg.ProcessMonit != nil {
		procMonit := cfg.ProcessMonit
		for _, pc := range procMonit.Monitors {
			m := monitor.NewProcessMonitor(pc, procMonit.CkTimeout, mailSender, stopCh)
			monitors = append(monitors, m)
			logger.Infof("Append process monitor: %s -> %s", pc.Name, pc.Pattern)
		}
	}

	if cfg.MemMonit.Enable {
		monitors = append(monitors, monitor.NewMemoryUsageMonitor(*cfg.MemMonit, mailSender, stopCh))
		logger.Infof("Append memory monitor: interval=%ds, recheck_interval=%ds, recheck_cnt=%d",
			cfg.MemMonit.Interval, cfg.MemMonit.RecheckInterval, cfg.MemMonit.RecheckCnt)
	}
	if cfg.DiskMonit != nil {
		diskMonit := cfg.DiskMonit
		for _, dc := range diskMonit.Monitors {
			m := monitor.NewDiskUsageMonitor(dc, diskMonit.CkTimeout, mailSender, stopCh)
			monitors = append(monitors, m)
			logger.Infof("Append disk monitor: %s -> %s", dc.Name, dc.Path)
		}
	}

	if cfg.MysqlAliveMonit != nil && cfg.MysqlAliveMonit.Enable {
		monitors = append(monitors, monitor.NewMysqlAliveMonitor(*cfg.MysqlAliveMonit, mailSender, stopCh))
		logger.Infof("Append mysql alive monitor: port=%d", cfg.MysqlAliveMonit.Port)
	}
	if cfg.MysqlReplMonit != nil && cfg.MysqlReplMonit.Enable {
		monitors = append(monitors, monitor.NewMySQLReplMonitor(*cfg.MysqlReplMonit, mailSender, stopCh))
		logger.Infof("Append mysql replication monitor: %s:%d", cfg.MysqlReplMonit.IP, cfg.MysqlReplMonit.Port)
	}

	for _, m := range monitors {
		m.Start()
	}

	<-stopCh

	logger.Info("Main Waiting for monitor threads...")
	for _, m := range monitors {
		logger.Infof("Begin join %s", m.ThreadName())
		if !m.Join(joinTimeout) {
			logger.Warnf("%s still alive after timeout", m.ThreadName())
		}
	}
	logger.Info("Main All monitor threads stopped")
	logger.Info("Main Stopped Done")
	return nil
}

func signalName(sig os.Signal) string {
	switch sig {
	case syscall.SIGTERM:
		return "SIGTERM"
	case os.Interrupt:
		return "SIGINT"
	default:
		return sig.String()
	}
}
