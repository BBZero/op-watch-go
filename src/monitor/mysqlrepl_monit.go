package monitor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	mysqldrv "github.com/go-sql-driver/mysql"

	"github.com/BBZero/op-watch-go/config"
	"github.com/BBZero/op-watch-go/logger"
)

const (
	mysqlReplMonitTag     = "MysqlReplMonit"
	checkCountLogInterval = 100

	mysqlReplStateRecover = "recover"
	mysqlReplStateFail    = "fail"

	replStatusOK               = "OK"
	replStatusReplicationError = "REPLICATION_ERROR"
	replStatusMysqlDown        = "MYSQL_DOWN"
	replStatusAuthError        = "AUTH_ERROR"
	replStatusNetworkError     = "NETWORK_ERROR"
	replStatusMysqlError       = "MYSQL_ERROR"
)

// replResult mirrors python ReplicationResult; nil fields are shown as "None".
type replResult struct {
	status  string
	message string

	ioRunning        *string
	sqlRunning       *string
	secondsBehindSrc *string
	sourceHost       *string
	sourcePort       *string
	sourceUUID       *string
	lastIOError      *string
	lastIOErrno      *string
	lastSQLError     *string
	lastSQLErrno     *string
	retrievedGtidSet *string
	executedGtidSet  *string
}

func strOrNone(s *string) string {
	if s == nil {
		return "None"
	}
	return *s
}

type MySQLReplMonitor struct {
	cfg        config.MysqlReplMonitConfig
	mailSender *MailSender
	stopCh     <-chan struct{}
	done       chan struct{}
	logtag     string

	checkCount int
	failCount  int
	downCount  int
	inFailure  bool
}

func NewMySQLReplMonitor(mc config.MysqlReplMonitConfig, mailSender *MailSender, stopCh <-chan struct{}) *MySQLReplMonitor {
	return &MySQLReplMonitor{
		cfg:        mc,
		mailSender: mailSender,
		stopCh:     stopCh,
		done:       make(chan struct{}),
		logtag:     "[" + mysqlReplMonitTag + "]",
	}
}

func (m *MySQLReplMonitor) ThreadName() string { return mysqlReplMonitTag }

func (m *MySQLReplMonitor) Start() { go m.run() }

func (m *MySQLReplMonitor) Join(timeout time.Duration) bool {
	select {
	case <-m.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (m *MySQLReplMonitor) run() {
	logger.Infof("%s monitor thread started: interval=%d, cycle=%d, recheck_delay=%d, account=%s, ip=%s, port=%d",
		m.logtag, m.cfg.Interval, m.cfg.Cycle, m.cfg.RecheckDelay, m.cfg.Account, m.cfg.IP, m.cfg.Port)
	defer func() {
		logger.Infof("%s MySQL replication monitor stopped", m.logtag)
		close(m.done)
	}()
	interval := time.Duration(m.cfg.Interval) * time.Second
	for {
		select {
		case <-m.stopCh:
			return
		default:
		}
		m.checkOnce()
		timer := time.NewTimer(interval)
		select {
		case <-m.stopCh:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func isMysqlAlive() bool {
	return exec.Command("pgrep", "-x", "mysqld").Run() == nil
}

func classifyMySQLErr(err error) *replResult {
	var me *mysqldrv.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case 1044, 1045, 1698:
			return &replResult{status: replStatusAuthError, message: fmt.Sprintf("MySQL authentication error: %v", err)}
		}
		return &replResult{status: replStatusMysqlError, message: fmt.Sprintf("MySQL error: %v", err)}
	}
	return &replResult{status: replStatusNetworkError, message: fmt.Sprintf("MySQL connection error: %v", err)}
}

// connect returns a db handle or a failed result; it only connects, nothing else.
func (m *MySQLReplMonitor) connect() (*sql.DB, *replResult) {
	host := m.cfg.IP
	if host == "127.0.0.1" || strings.EqualFold(host, "localhost") {
		if !isMysqlAlive() {
			return nil, &replResult{status: replStatusMysqlDown, message: "MySQL is not running"}
		}
	}
	timeout := time.Duration(m.cfg.ConnectTimeout) * time.Second
	dc := mysqldrv.NewConfig()
	dc.User = m.cfg.Account
	dc.Passwd = m.cfg.Pwd
	dc.Net = "tcp"
	dc.Addr = net.JoinHostPort(host, strconv.Itoa(m.cfg.Port))
	dc.Timeout = timeout
	dc.ReadTimeout = timeout
	dc.WriteTimeout = timeout
	connector, err := mysqldrv.NewConnector(dc)
	if err != nil {
		return nil, &replResult{status: replStatusMysqlError, message: fmt.Sprintf("Unexpected error: %v", err)}
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, classifyMySQLErr(err)
	}
	return db, nil
}

func pickCol(row map[string]sql.NullString, keys ...string) *string {
	for _, k := range keys {
		if v, ok := row[k]; ok && v.Valid {
			s := v.String
			return &s
		}
	}
	return nil
}

func (m *MySQLReplMonitor) checkRepl(db *sql.DB) *replResult {
	timeout := time.Duration(m.cfg.ConnectTimeout) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// MySQL 8.0.22+ uses SHOW REPLICA STATUS, older versions use SHOW SLAVE STATUS.
	rows, err := db.QueryContext(ctx, "SHOW REPLICA STATUS")
	if err != nil {
		var me *mysqldrv.MySQLError
		if errors.As(err, &me) && (me.Number == 1064 || me.Number == 1287) {
			rows, err = db.QueryContext(ctx, "SHOW SLAVE STATUS")
		}
		if err != nil {
			return classifyMySQLErr(err)
		}
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return classifyMySQLErr(err)
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return classifyMySQLErr(err)
		}
		return &replResult{status: replStatusReplicationError, message: "Replication is not configured"}
	}
	vals := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return classifyMySQLErr(err)
	}
	row := make(map[string]sql.NullString, len(cols))
	for i, c := range cols {
		row[c] = vals[i]
	}

	ioRunning := pickCol(row, "Replica_IO_Running", "Slave_IO_Running")
	sqlRunning := pickCol(row, "Replica_SQL_Running", "Slave_SQL_Running")
	res := &replResult{
		ioRunning:        ioRunning,
		sqlRunning:       sqlRunning,
		secondsBehindSrc: pickCol(row, "Seconds_Behind_Source", "Seconds_Behind_Master"),
		sourceHost:       pickCol(row, "Source_Host", "Master_Host"),
		sourcePort:       pickCol(row, "Source_Port", "Master_Port"),
		sourceUUID:       pickCol(row, "Source_UUID", "Master_UUID"),
		retrievedGtidSet: pickCol(row, "Retrieved_Gtid_Set"),
		executedGtidSet:  pickCol(row, "Executed_Gtid_Set"),
	}
	ioRun := ioRunning != nil && strings.EqualFold(*ioRunning, "yes")
	sqlRun := sqlRunning != nil && strings.EqualFold(*sqlRunning, "yes")
	if ioRun && sqlRun {
		res.status = replStatusOK
		res.message = "Replication is OK"
		return res
	}
	res.status = replStatusReplicationError
	res.message = "Replication is not OK"
	res.lastIOError = pickCol(row, "Last_IO_Error")
	res.lastIOErrno = pickCol(row, "Last_IO_Errno")
	res.lastSQLError = pickCol(row, "Last_SQL_Error")
	res.lastSQLErrno = pickCol(row, "Last_SQL_Errno")
	return res
}

func (m *MySQLReplMonitor) notify(state string, r *replResult) {
	if m.mailSender == nil {
		return
	}
	level, title := "info", ""
	switch state {
	case mysqlReplStateFail:
		level = "error"
		title = "MySQL replication unusual"
	case mysqlReplStateRecover:
		title = "MySQL replication recovery"
	}
	var rows [][2]string
	switch r.status {
	case replStatusNetworkError, replStatusMysqlDown, replStatusAuthError:
	default:
		rows = [][2]string{
			{"Status", r.status},
			{"IO_Running", strOrNone(r.ioRunning)},
			{"SQL_Running", strOrNone(r.sqlRunning)},
			{"Seconds_Behind_Source", strOrNone(r.secondsBehindSrc)},
			{"Source_Host", strOrNone(r.sourceHost)},
			{"Source_Port", strOrNone(r.sourcePort)},
			{"Source_UUID", strOrNone(r.sourceUUID)},
			{"Last_IO_Error", strOrNone(r.lastIOError)},
			{"Last_IO_Errno", strOrNone(r.lastIOErrno)},
			{"Last_SQL_Error", strOrNone(r.lastSQLError)},
			{"Last_SQL_Errno", strOrNone(r.lastSQLErrno)},
			{"Retrieved_Gtid_Set", strOrNone(r.retrievedGtidSet)},
			{"Executed_Gtid_Set", strOrNone(r.executedGtidSet)},
		}
	}
	m.mailSender.sendNormalNoti(level, "", title, r.message,
		flagPrefix+"-repl-"+strconv.Itoa(m.checkCount), m.logtag, rows)
}

func (m *MySQLReplMonitor) checkOnce() {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("%s unexpected exception in monitor: %v", m.logtag, r)
		}
	}()
	m.checkCount++

	db, result := m.connect()
	if db != nil {
		defer db.Close()
		result = m.checkRepl(db)
		// Replication error -> double check after a delay.
		delay := m.cfg.RecheckDelay
		if result.status == replStatusReplicationError && delay > 0 {
			if m.checkCount%checkCountLogInterval == 0 || m.checkCount < 10 || m.failCount < m.cfg.Cycle {
				logger.Infof("%s Check Cnt: %d, Status: %s, fail_cnt: %d, cycle: %d, IO_Run: %s, SQL_Run: %s, will recheck in %d seconds",
					m.logtag, m.checkCount, result.status, m.failCount, m.cfg.Cycle,
					strOrNone(result.ioRunning), strOrNone(result.sqlRunning), delay)
			}
			timer := time.NewTimer(time.Duration(delay) * time.Second)
			select {
			case <-m.stopCh:
				timer.Stop()
			case <-timer.C:
				result = m.checkRepl(db)
			}
		}
	}

	m.handleResult(result)
}

func (m *MySQLReplMonitor) handleResult(result *replResult) {
	hasPrintLog := false
	switch result.status {
	case replStatusMysqlDown:
		// MySQL being down is not a replication failure; keep inFailure so recovery
		// is only reported once replication is actually OK.
		m.failCount = 0
		if m.downCount == 0 {
			logger.Infof("%s Check Cnt: %d, Status: %s", m.logtag, m.checkCount, result.status)
			hasPrintLog = true
		}
		m.downCount++
	case replStatusOK:
		m.downCount = 0
		if m.inFailure {
			logger.Infof("%s Check Cnt: %d, Status: %s, will noti", m.logtag, m.checkCount, result.status)
			hasPrintLog = true
			m.notify(mysqlReplStateRecover, result)
		}
		m.failCount = 0
		m.inFailure = false
	case replStatusReplicationError:
		m.failCount++
		m.downCount = 0
		cycle := m.cfg.Cycle
		detail := fmt.Sprintf("IO_Run: %s, SQL_Run: %s, Sec_Behind: %s",
			strOrNone(result.ioRunning), strOrNone(result.sqlRunning), strOrNone(result.secondsBehindSrc))
		if m.failCount == cycle && !m.inFailure {
			logger.Infof("%s Check Cnt: %d, Status: %s, reached cycle %d: %s, will noti",
				m.logtag, m.checkCount, result.status, cycle, detail)
			m.notify(mysqlReplStateFail, result)
			hasPrintLog = true
			m.inFailure = true
		} else if m.failCount == 1 {
			logger.Infof("%s Check Cnt: %d, Status: %s, failed (1/%d): %s",
				m.logtag, m.checkCount, result.status, cycle, detail)
			hasPrintLog = true
		}
	default:
		m.downCount = 0
	}

	if !hasPrintLog && (m.checkCount%checkCountLogInterval == 0 || m.checkCount < 10) {
		logger.Infof("%s Check Cnt: %d, Status: %s, IO_Run: %s, SQL_Run: %s, Sec_Behind: %s",
			m.logtag, m.checkCount, result.status,
			strOrNone(result.ioRunning), strOrNone(result.sqlRunning), strOrNone(result.secondsBehindSrc))
	}

	switch result.status {
	case replStatusAuthError, replStatusNetworkError, replStatusMysqlError:
		logger.Errorf("%s Fatal MySQL error: %s", m.logtag, result.message)
	}
}
