package config

// Config is the JSON configuration for opwatch.
type Config struct {
	BaseConfig
	Log          LogConfig            `json:"log"`
	ProcessMonit *ProcessMonitSection `json:"process_monit"`
	DiskMonit    *DiskMonitSection    `json:"disk_monit"`
	MemMonit     *MemMonitConfig      `json:"mem_monit"`
	MysqlReplMonit *MysqlReplMonitConfig `json:"mysqlrepl_monit"`
	MysqlAliveMonit *MysqlAliveMonitConfig `json:"mysqlalive_monit"`
}

type BaseConfig struct {
	Enable         bool   `json:"enable"`
	MachineUpdelay int    `json:"machine_updelay"`
	EnableNotify   bool   `json:"enable_noti"`
	MailCmd        string `json:"mail_command"`
}

type ProcessMonitSection struct {
	Interval  int                  `json:"interval"`
	CkTimeout int                  `json:"ck_timeout"`
	Cycle     int                  `json:"cycle"`
	Monitors  []ProcessMonitConfig `json:"monitors"`
}

type ProcessMonitConfig struct {
	Name      string `json:"name"`
	Pattern   string `json:"pattern"`
	Interval  int    `json:"-"` // filled from process_monit.interval on load
	FailCycle int    `json:"-"` // filled from process_monit.cycle on load
}

type DiskMonitSection struct {
	Interval  int               `json:"interval"`
	CkTimeout int               `json:"ck_timeout"`
	Cycle     int               `json:"cycle"`
	Monitors  []DiskMonitConfig `json:"monitors"`
}

type DiskMonitConfig struct {
	Name             string  `json:"name"`
	Path             string  `json:"path"`
	ExceedThreshold  float64 `json:"exceed_threshold"`
	RecoverThreshold float64 `json:"recover_threshold"`
	Interval         int     `json:"-"` // filled from disk_monit.interval on load
	FailCycle        int     `json:"-"` // filled from disk_monit.cycle on load
}

type MemMonitConfig struct {
	Name             string  `json:"-"`
	Enable           bool    `json:"enable"`
	WarnThreshold    float64 `json:"warn_threshold"`
	RecoverThreshold float64 `json:"recover_threshold"`
	Interval         int     `json:"interval"`
	RecheckInterval  int     `json:"recheck_interval"`
	RecheckCnt       int     `json:"recheck_cnt"`
	// Absolute available-memory floor, fixed default on load (not configurable)
	WarnAvailableBytes int64 `json:"-"`
}
type MysqlAliveMonitConfig struct {
	Enable          bool `json:"enable"`
	Interval        int  `json:"interval"`
	RecheckInterval int  `json:"recheck_interval"`
	RecheckCnt      int  `json:"recheck_cnt"`
	Port            int  `json:"port"`
}

type MysqlReplMonitConfig struct {
	Enable         bool   `json:"enable"`
	Interval       int    `json:"interval"`
	Cycle          int    `json:"cycle"`
	RecheckDelay   int    `json:"recheck_delay"`
	Account        string `json:"account"`
	Pwd            string `json:"pwd"`
	IP             string `json:"ip"`
	Port           int    `json:"port"`
	ConnectTimeout int    `json:"connect_timeout"`
}

type LogConfig struct {
	Path    string `json:"path"`
	Level   string `json:"level"`
	MaxSize int    `json:"maxsize"`
	MaxNum  int    `json:"maxnum"`
}
