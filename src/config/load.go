package config

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	DefaultProcessInterval  = 30
	DefaultDiskInterval     = 120
	DefaultMachineUpdelay   = 20
	DefaultProcessCycle     = 1
	DefaultDiskCycle        = 1
	DefaultDiskCkTimeout    = 10
	DefaultProcessCkTimeout = 10
	DefaultEnable           = true
	DefaultEnableNoti       = true

	DefaultMemInterval         = 60
	DefaultMemRecheckInterval  = 10
	DefaultMemRecheckCnt       = 3
	DefaultMemWarnThreshold    = 80.0
	DefaultMemRecoverThreshold = 75.0
	DefaultMemWarnAvailBytes   = 256 * 1024 * 1024

	MaxMemInterval        = 3600
	MaxMemRecheckInterval = 600
	MaxMemRecheckCnt      = 1000

	DefaultLogLevel   = "INFO"
	DefaultLogMaxSize = 10
	DefaultLogMaxNum  = 5

	maxConfigFileSize = 1024 * 1024

	MaxInterval     = 2 * 60 * 60 //2h
	MaxCheckTimeout = 60

	MinProcessInterval = 2
	MinDiskInterval    = 5

	MinDiskCheckTimeout    = 5
	MinProcessCheckTimeout = 5

	DefaultMysqlReplInterval   = 300
	MinMysqlReplInterval       = 120
	MaxMysqlReplInterval       = 24 * 60 * 60
	DefaultMysqlReplCycle      = 1
	MaxMysqlReplCycle          = 100
	MinMysqlReplRecheckDelay   = 20
	MaxMysqlReplRecheckDelay   = 180
	DefaultMysqlConnectTimeout = 10
	DefaultMysqlPort           = 3306

	DefaultMysqlAliveInterval        = 30
	DefaultMysqlAliveRecheckInterval = 10
	DefaultMysqlAliveRecheckCnt      = 3
	MaxInterval24h                   = 24 * 60 * 60
	DefaultMysqlIP                   = "127.0.0.1"
)

func checkCfgFile(path string) error {
	fileinfo, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s", path)
		}
		return fmt.Errorf("failed to stat config file %s: %w", path, err)
	}
	if !fileinfo.Mode().IsRegular() {
		return fmt.Errorf("config path is not a file: %s", path)
	}
	const maxConfigSize = 1024 * 1024 // 1MB
	if fileinfo.Size() > maxConfigSize {
		return fmt.Errorf("config file too big (%d bytes): %s", fileinfo.Size(), path)
	}
	return nil
}
func stripCommentLines(r io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" ||
			strings.HasPrefix(trimmed, "//") ||
			strings.HasPrefix(trimmed, "#") {
			continue
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func clamp(val, min, max, defaultval int) int {
	if val <= 0 {
		return defaultval
	}
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

// Load reads and parses the JSON config file. Missing or invalid values fall
// back to defaults; invalid disk monitor thresholds abort with an error.
func Load(path string) (*Config, error) {
	if err := checkCfgFile(path); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %v", err)
	}
	filtered, errFilter := stripCommentLines(bytes.NewReader(data))
	if errFilter != nil {
		return nil, errFilter
	}

	cfg := defaultConfig()
	if err := json.Unmarshal(filtered, cfg); err != nil {
		return nil, fmt.Errorf("failed to read config: %v", err)
	}

	// check and validate parameter
	if cfg.ProcessMonit != nil {
		cfg.ProcessMonit.Interval = clamp(cfg.ProcessMonit.Interval,
			MinProcessInterval, MaxInterval, DefaultProcessInterval)
		cfg.ProcessMonit.CkTimeout = clamp(cfg.ProcessMonit.CkTimeout,
			MinProcessCheckTimeout, MaxCheckTimeout, DefaultProcessCkTimeout)
		cfg.ProcessMonit.Cycle = clamp(cfg.ProcessMonit.Cycle,
			1, 1000, DefaultProcessCycle)
	}

	if cfg.DiskMonit != nil {
		cfg.DiskMonit.Interval = clamp(cfg.DiskMonit.Interval,
			MinDiskInterval, MaxInterval, DefaultDiskInterval)
		cfg.DiskMonit.CkTimeout = clamp(cfg.DiskMonit.CkTimeout,
			MinDiskCheckTimeout, MaxCheckTimeout, DefaultDiskCkTimeout)
		cfg.DiskMonit.Cycle = clamp(cfg.DiskMonit.Cycle,
			1, 1000, DefaultDiskCycle)
	}

	if cfg.MachineUpdelay < 0 {
		cfg.MachineUpdelay = 0
	} else if cfg.MachineUpdelay > 120 {
		cfg.MachineUpdelay = 120
	}

	if cfg.Log.Level == "" {
		cfg.Log.Level = DefaultLogLevel
	}
	if cfg.Log.MaxSize <= 0 {
		cfg.Log.MaxSize = DefaultLogMaxSize
	} else if cfg.Log.MaxSize > 1024 {
		cfg.Log.MaxSize = 1024
	}

	if cfg.Log.MaxNum <= 0 {
		cfg.Log.MaxNum = DefaultLogMaxNum
	} else if cfg.Log.MaxNum > 20 {
		cfg.Log.MaxNum = 20
	}

	if cfg.MemMonit != nil {
		mem := cfg.MemMonit
		mem.Name = "Memory"
		mem.WarnAvailableBytes = DefaultMemWarnAvailBytes
		mem.Interval = clamp(mem.Interval, 1, MaxMemInterval, DefaultMemInterval)
		mem.RecheckInterval = clamp(mem.RecheckInterval, 1, MaxMemRecheckInterval, DefaultMemRecheckInterval)
		mem.RecheckCnt = clamp(mem.RecheckCnt, 1, MaxMemRecheckCnt, DefaultMemRecheckCnt)
		if mem.RecoverThreshold < 5 || mem.RecoverThreshold > 99 ||
			mem.WarnThreshold < 5 || mem.WarnThreshold > 99 {
			return nil, fmt.Errorf("memory thresholds must be in the range [5, 99]: warn=%v, recover=%v",
				mem.WarnThreshold, mem.RecoverThreshold)
		}
		if mem.RecoverThreshold+1 >= mem.WarnThreshold {
			return nil, fmt.Errorf("invalid memory thresholds: expected recover + 1 <= warn, got warn=%v, recover=%v",
				mem.WarnThreshold, mem.RecoverThreshold)
		}
	}

	if cfg.MysqlAliveMonit != nil {
		a := cfg.MysqlAliveMonit
		a.Interval = clamp(a.Interval, 1, MaxInterval24h, DefaultMysqlAliveInterval)
		a.RecheckInterval = clamp(a.RecheckInterval, 1, 60, DefaultMysqlAliveRecheckInterval)
		a.RecheckCnt = clamp(a.RecheckCnt, 2, 10, DefaultMysqlAliveRecheckCnt)
		a.Port = clamp(a.Port, 1, 65535, DefaultMysqlPort)
	}

	if cfg.MysqlReplMonit != nil {
		r := cfg.MysqlReplMonit
		r.Interval = clamp(r.Interval, MinMysqlReplInterval, MaxMysqlReplInterval, DefaultMysqlReplInterval)
		r.Cycle = clamp(r.Cycle, 1, MaxMysqlReplCycle, DefaultMysqlReplCycle)
		// 0 disables the double check
		if r.RecheckDelay < 0 {
			r.RecheckDelay = 0
		} else if r.RecheckDelay > 0 {
			r.RecheckDelay = clamp(r.RecheckDelay, MinMysqlReplRecheckDelay, MaxMysqlReplRecheckDelay, MinMysqlReplRecheckDelay)
		}
		r.ConnectTimeout = clamp(r.ConnectTimeout, 1, MaxCheckTimeout, DefaultMysqlConnectTimeout)
		r.Port = clamp(r.Port, 1, 65535, DefaultMysqlPort)
		r.IP = strings.TrimSpace(r.IP)
		if r.IP == "" {
			r.IP = DefaultMysqlIP
		}
	}

	if cfg.ProcessMonit != nil && len(cfg.ProcessMonit.Monitors) > 0 {
		var processes []ProcessMonitConfig
		for i, pc := range cfg.ProcessMonit.Monitors {
			pc.Name = strings.TrimSpace(pc.Name)
			pc.Pattern = strings.TrimSpace(pc.Pattern)
			if pc.Name == "" {
				fmt.Printf("Invalid process monitor name (index %d) ignored\n", i)
				continue
			}
			if pc.Pattern == "" {
				fmt.Printf("Empty process match for %s ignored\n", pc.Name)
				continue
			}
			pc.Interval = cfg.ProcessMonit.Interval
			pc.FailCycle = cfg.ProcessMonit.Cycle
			processes = append(processes, pc)
		}
		cfg.ProcessMonit.Monitors = processes
	}

	if cfg.DiskMonit != nil && len(cfg.DiskMonit.Monitors) > 0 {
		var disks []DiskMonitConfig
		for i, dc := range cfg.DiskMonit.Monitors {
			dc.Name = strings.TrimSpace(dc.Name)
			dc.Path = strings.TrimSpace(dc.Path)
			if dc.Name == "" {
				fmt.Printf("Invalid disk monitor name (index %d) ignored\n", i)
				continue
			}
			if dc.Path == "" {
				fmt.Printf("Empty disk path for %s ignored\n", dc.Name)
				continue
			}
			if dc.ExceedThreshold < 1 || dc.ExceedThreshold > 99 ||
				dc.RecoverThreshold < 1 || dc.RecoverThreshold > 99 {
				fmt.Printf("Invalid threshold for %s, must be 1-99: warn=%v, recover=%v\n",
					dc.Name, dc.ExceedThreshold, dc.RecoverThreshold)
				return nil, fmt.Errorf("invalid threshold for %s, must be 1-99: warn=%v, recover=%v",
					dc.Name, dc.ExceedThreshold, dc.RecoverThreshold)
			}
			if dc.RecoverThreshold >= dc.ExceedThreshold {
				fmt.Printf("Invalid threshold for %s, recover (%v) must be less than warn (%v)\n",
					dc.Name, dc.RecoverThreshold, dc.ExceedThreshold)
				return nil, fmt.Errorf("invalid threshold for %s, recover (%v) must be less than warn (%v)",
					dc.Name, dc.RecoverThreshold, dc.ExceedThreshold)
			}
			dc.Interval = cfg.DiskMonit.Interval
			dc.FailCycle = cfg.DiskMonit.Cycle
			disks = append(disks, dc)
		}
		cfg.DiskMonit.Monitors = disks
	}

	return cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		BaseConfig: BaseConfig{
			Enable:         DefaultEnable,
			MachineUpdelay: DefaultMachineUpdelay,
			EnableNotify:   DefaultEnableNoti,
		},
		Log: LogConfig{Level: DefaultLogLevel, MaxSize: DefaultLogMaxSize, MaxNum: DefaultLogMaxNum},
	}
}

// // Memory monitor is disabled unless explicitly enabled; missing fields keep these defaults.
// func defaultMemConfig() *MemMonitConfig {
// 	return &MemMonitConfig{
// 		Enable:           false,
// 		WarnThreshold:    DefaultMemWarnThreshold,
// 		RecoverThreshold: DefaultMemRecoverThreshold,
// 		Interval:         DefaultMemInterval,
// 		RecheckInterval:  DefaultMemRecheckInterval,
// 		RecheckCnt:       DefaultMemRecheckCnt,
// 	}
// }
