# OpWatch 使用說明

OpWatch 是一個輕量級主機監控工具，透過 OpenMailer 發送通知，支援四類監控：

| 監控 | 配置區段 | 說明 |
|---|---|---|
| 進程 | `process_monit` | 以 `pgrep -f` 監控任意命令列，回報 stopped / started / restarted |
| 磁碟 | `disk_monit` | 監控掛載點或區塊裝置的使用率，回報 exceed / recover / ckfail |
| 記憶體 | `mem_monit` | 監控實體記憶體使用率，回報超限 / 恢復 / 檢測失敗 |
| MySQL 存活 | `mysqlalive_monit` | 監控本機 `mysqld` 進程與監聽埠 |
| MySQL 複製 | `mysqlrepl_monit` | 監控 MySQL replication 狀態，回報異常 / 恢復 |

## 安裝與服務管理

```bash
sudo apt install ./opwatch_<version>_amd64.deb   # 安裝（會自動建立 opwatch 系統帳號）
sudo systemctl start opwatch                     # 啟動
sudo systemctl status opwatch                    # 狀態
sudo systemctl restart opwatch                   # 重啟（改完設定後）
journalctl -u opwatch -f                         # 看即時日誌
```

首次安裝後服務**不會自動啟動**，請先修改設定檔再手動 `start`。

命令列參數：

```bash
opwatch run -c /etc/opwatch/watch.conf   # 指定設定檔執行
opwatch version                          # 輸出版本
```

`-c` 省略時，預設讀取執行檔同目錄下的 `watch.json`；若取不到執行檔路徑則回退到當前工作目錄的 `watch.json`。

## 配置檔

- 路徑：`/etc/opwatch/watch.conf`（conffile，升級不覆蓋本地修改）
- 格式：JSON，支援 `//` 與 `#` 開頭的註釋行及空行（載入時會先剔除）
- 修改後需 `systemctl restart opwatch` 生效
- 數值超出範圍不會報錯，會自動夾到合法區間或回退預設值（下文各表已標明範圍）

### 通用設定

| 鍵 | 預設 | 說明 |
|---|---|---|
| `enable` | `true` | 總開關，`false` 時載入設定後直接退出 |
| `enable_noti` | `true` | 通知總開關，`false` 時所有監控只記日誌不發信 |
| `machine_updelay` | `20` | 開機後延遲多少秒才開始監控（0–120，依 `/proc/uptime` 計算） |
| `mail_command` | — | OpenMailer 命令路徑，如 `/usr/local/bin/openmailer`；為空則跳過發信並記警告 |
| `log.path` | 空 | **日誌檔案的完整路徑**（不是目錄），如 `/var/log/opwatch/opwatch.log`；留空只輸出到 stdout（走 journald） |
| `log.level` | `INFO` | `DEBUG` / `INFO` / `WARN` / `ERROR` |
| `log.maxsize` / `log.maxnum` | `10` / `5` | 單檔大小（MB，上限 1024）／保留份數（上限 20） |

## 進程監控

### 偵測機制

每個監控項以 `pgrep -f <比對字串>` 搜尋**完整命令列**（full command line），單次檢查最多等待 `ck_timeout` 秒（預設 10，範圍 5–60），採用第一個回傳的 PID。

`process_monit` 區段設定：

| 鍵 | 預設 | 說明 |
|---|---|---|
| `interval` | `30` | 檢查間隔秒數（最小 2，最大 7200） |
| `ck_timeout` | `10` | 單次 pgrep 逾時秒數（5–60） |
| `cycle` | `1` | 連續失敗幾次才觸發 STOPPED 通知（1–1000） |
| `monitors[].name` | — | 監控項名稱（通知中標識用） |
| `monitors[].pattern` | — | pgrep -f 的比對字串 |

### 通知事件

一共會發送三種通知：`STOPPED`、`STARTED`、`RESTARTED`。

首次檢查到進程執行時只記錄日誌；連續失敗達到 `cycle` 後發送一次 STOPPED 通知。進程恢復且先前已發出停止通知時發送 STARTED；進程持續執行但 PID 改變時發送 RESTARTED。

### 通知規則細節

**STOPPED 通知**
- 進程必須持續處於 not run 狀態，累計失敗次數達到設定的 Cycle，才發送 STOPPED 通知。
- 若進程在達到 Cycle 前恢復運行，則不發送 STOPPED 通知。

**STARTED 通知**
- 只有先前已發送過 STOPPED 通知，且進程隨後恢復運行時，才發送 STARTED 通知。
- 若先前未發送 STOPPED 通知，即使進程恢復運行，也不發送 STARTED 通知。

**RESTARTED 通知**
- 偵測到 PID 發生變化時，表示進程已重新啟動，發送 RESTARTED 通知。
- 此規則不受先前是否發送過 STOPPED 通知影響。
- 若進程在未達 Cycle 前停止並恢復運行，但 PID 已改變，仍發送 RESTARTED 通知，而非 STARTED 通知。

**不發送通知**
- 若進程在未達 Cycle 前停止並恢復運行，且 PID 沒有變化，不發送任何通知。
- Monitor 啟動後第一次偵測到進程正在運行、且先前未曾發送過 STOPPED 通知時，不發送任何通知。

### 通知示例

假設設定 `interval=10`、`cycle=3`，監控 nginx：

| 時間 | 檢查結果 | 說明 | 發送的通知 |
|---|---|---|---|
| t=10s | running，pid=1000 | 啟動後首次檢查到執行 | 無（只記日誌） |
| t=20s~40s | running，pid=1000 | 持續正常 | 無 |
| t=50s | not run | 失敗 1/3 | 無（未達 Cycle） |
| t=60s | not run | 失敗 2/3 | 無（未達 Cycle） |
| t=70s | not run | 失敗 3/3 | **STOPPED** |
| t=80s~90s | not run | 仍在停止，不重複告警 | 無（已發過 STOPPED） |
| t=100s | running，pid=2000 | 恢復運行 | **STARTED**（先前已發過 STOPPED） |
| t=110s | running，pid=3000 | PID 改變（被重啟） | **RESTARTED** |
| t=120s | not run | 失敗 1/3 | 無（未達 Cycle） |
| t=130s | running，pid=4000 | 未達 Cycle 即恢復，但 PID 已變 | **RESTARTED**（非 STARTED） |

若 t=120s 停止後在 t=130s 恢復且 **PID 不變**（例如進程自己 fork 回來、PID 復用），則 t=130s 不發送任何通知。

### 通知的發送方式

```bash
<mail_command> send \
  --type process \
  --name <監控項名稱> \
  --flag oppmonit-p-<檢查序號> \
  --event stopped|started|restarted \
  --caller op-watch \
  --sender "OpWatch(Go)" \
  --message "<詳細訊息>" \
  [--pid <PID>]
```

`--message` 內容範例（STOPPED 事件）：`Fail Count: 3, Check Count: 7, Pattern: nginx`。

## 磁碟監控

### 偵測機制

`path` 可以是掛載點（如 `/`、`/data`），也可以是區塊裝置（如 `/dev/sda1`、`/dev/disk/by-uuid/...`、`/dev/mapper/...`）。若為區塊裝置，會讀 `/proc/mounts` 以裝置號（rdev）比對找出實際掛載點；找不到掛載點則視為檢查失敗（ckfail）。

使用率計算與 `df` / `shutil.disk_usage` 一致：

```
total = f_blocks * f_frsize
used  = (f_blocks - f_bfree) * f_frsize    # 含保留區塊
avail = f_bavail * f_frsize                # 一般使用者可用
usage_pct = used / (used + avail) * 100
```

`disk_monit` 區段設定：

| 鍵 | 預設 | 說明 |
|---|---|---|
| `interval` | `120` | 檢查間隔秒數（最小 5，最大 7200） |
| `ck_timeout` | `10` | 單次檢查逾時秒數（5–60，目前保留未使用） |
| `cycle` | `1` | 連續超限幾次才觸發 EXCEED 通知（1–1000） |
| `monitors[].name` | — | 監控項名稱 |
| `monitors[].path` | — | 掛載點或區塊裝置路徑 |
| `monitors[].exceed_threshold` | — | 超限閾值百分比（1–99，必填） |
| `monitors[].recover_threshold` | — | 恢復閾值百分比（1–99，必填，且必須小於 `exceed_threshold`） |

### 狀態與通知規則

每次檢查落在以下四種狀態之一：

| 狀態 | 條件 |
|---|---|
| `exceed` | `usage_pct >= exceed_threshold` |
| `recover` | `usage_pct < recover_threshold` |
| `ok` | 介於兩者之間 |
| `ckfail` | 路徑不存在、裝置未掛載、`statfs` 失敗等 |

通知規則：

- **EXCEED**：連續 `cycle` 次處於 `exceed`，且本輪尚未發過超限通知時，發送一次。發送後在 `recover <= usage < exceed` 的滯回區間內不會重置累計次數，避免同一波超限重複告警；必須先進入 `recover` 才會解除。
- **RECOVER**：先前已發送過 EXCEED，且本次進入 `recover` 時，發送恢復通知。
- **CKFAIL**：從非 `ckfail` 首次轉為 `ckfail` 時發送一次；持續失敗不重複發送。
- 未達 `cycle` 就回落，不發送任何超限通知。

### 通知的發送方式

```bash
<mail_command> send \
  --type disk_usage \
  --flag oppmonit-d-<檢查序號> \
  --state exceed|recover|ckfail \
  --path <設定中的 path> \
  --caller op-watch \
  --sender "OpWatch(Go)" \
  --message "<詳細訊息>" \
  --used-percent <百分比> --used-bytes <bytes> --total-bytes <bytes> \
  --free-bytes <bytes> --avail-bytes <bytes> [--threshold-percent <exceed_threshold>]
```

- `--message` 範例：EXCEED 為 `Exceed Count = 3. Check Count: 5`；RECOVER 為 `恢復閾值: 70.00%. Check Count: 12`；CKFAIL 為 `Disk check failed: Path does not exist. Check Count: 2`。
- `ckfail` 時不帶 `--threshold-percent`，其餘數值欄位也可能缺省。

## 記憶體監控

### 偵測機制

每次讀取實體記憶體總量與可用量：

```
used = total - available
usage_pct = used / total * 100
```

觸發警告需滿足**任一**條件：

- `usage_pct >= warn_threshold`
- `available <= 256 MB`（固定的絕對下限，不可配置）

警戒解除需**同時**滿足：

- `usage_pct <= recover_threshold`
- `available > 256 MB`

狀態不會立即切換，必須連續 `recheck_cnt` 次檢查結果一致才確認；期間改用 `recheck_interval` 縮短間隔重查，中途結果反轉則取消本次轉換。

`mem_monit` 區段設定（整個區段存在時才能啟動，見下方注意事項）：

| 鍵 | 預設 | 說明 |
|---|---|---|
| `enable` | `false` | 必須明確設為 `true` 才會啟動記憶體監控 |
| `warn_threshold` | `80` | 警告閾值百分比（5–99） |
| `recover_threshold` | `75` | 恢復閾值百分比（5–99），必須滿足 `recover + 1 <= warn` |
| `interval` | `60` | 正常檢查間隔秒數（1–3600） |
| `recheck_interval` | `10` | 待確認狀態時的重查間隔秒數（1–600） |
| `recheck_cnt` | `3` | 需連續幾次一致才確認狀態切換（1–1000） |

### 通知

- **超限**：`level=warn`，標題「記憶體使用超限」。
- **恢復**：`level=info`，標題「記憶體使用恢復」（僅在先前處於超限狀態時發送）。
- **檢測失敗**：讀取記憶體資訊失敗時，`level=error`，標題「記憶體檢測失敗」，只在進入失敗時發一次。

```bash
<mail_command> send \
  --type notify \
  --level warn|error|info \
  --subject "記憶體使用超限" \
  --title "<標題>" \
  --flag oppmonit-mem-<檢查序號> \
  --caller op-watch \
  --sender "OpWatch(Go)" \
  --message "<詳細訊息>" \
  --row "使用百分比,<值>" --row "已用,<值>" --row "總共,<值>" \
  --row "可用,<值>" --row "警告閾值,<值>" --row "纍計檢測次數,<n>"
```

> 注意：`mem_monit` 區段若完全省略，程式啟動時會存取空指標而異常退出。不需要記憶體監控時，請保留區段並設 `"enable": false`。

## MySQL 存活監控

### 偵測機制

兩個條件同時成立才算健康：

1. `pgrep -x mysqld` 找到進程（逾時 10 秒）。
2. `/proc/net/tcp` 或 `/proc/net/tcp6` 中存在 `LISTEN`（狀態 `0A`）且埠號等於 `port` 的條目。

`mysqlalive_monit` 區段設定：

| 鍵 | 預設 | 說明 |
|---|---|---|
| `enable` | `false` | 明確設為 `true` 才啟動 |
| `interval` | `30` | 正常檢查間隔秒數（1–86400） |
| `recheck_interval` | `10` | 不健康時的重查間隔秒數（1–60） |
| `recheck_cnt` | `3` | 連續幾次不健康才發 STOPPED（2–10） |
| `port` | `3306` | 要確認的監聽埠 |

### 通知規則

- **STOPPED**：連續 `recheck_cnt` 次不健康（進程不存在，或進程存在但埠未監聽）後發送一次；持續不健康期間不再處理。
- **STARTED**：先前發過 STOPPED，之後進程存在且埠已監聽時發送。
- **RESTARTED**：健康狀態下偵測到 PID 改變時發送。
- 掃描本身失敗（pgrep 逾時、非 0/1 退出碼、`/proc/net/tcp` 讀取失敗）只記日誌，不改變告警狀態。

### 通知的發送方式

```bash
<mail_command> send \
  --type process \
  --name mysqld \
  --flag oppmonit-mysqlalive-<檢查序號> \
  --event stopped|started|restarted \
  --caller op-watch \
  --sender "OpWatch(Go)" \
  --message "<詳細訊息>" \
  [--pid <PID>]
```

## MySQL 複製監控

### 偵測機制

1. 若 `ip` 是 `127.0.0.1` 或 `localhost`，先確認本機 `mysqld` 進程存在；不存在則記為 `MYSQL_DOWN`。
2. 以 `account` / `pwd` 建立 TCP 連線（`ip:port`，逾時 `connect_timeout`）。
3. 執行 `SHOW REPLICA STATUS`；若回報 1064 / 1287（舊版語法不認得），自動改用 `SHOW SLAVE STATUS`。
4. `Replica_IO_Running` 與 `Replica_SQL_Running`（舊版為 `Slave_*`）皆為 `Yes` 視為 `OK`，否則為 `REPLICATION_ERROR`。
5. 若結果為 `REPLICATION_ERROR` 且 `recheck_delay > 0`，等待後再查一次，避免瞬時抖動誤報。

結果狀態：

| 狀態 | 說明 |
|---|---|
| `OK` | IO 與 SQL 執行緒皆正常 |
| `REPLICATION_ERROR` | 複製異常，或未設定複製 |
| `MYSQL_DOWN` | 本機 MySQL 未運行 |
| `AUTH_ERROR` | 認證失敗（errno 1044 / 1045 / 1698） |
| `NETWORK_ERROR` | 連線層錯誤 |
| `MYSQL_ERROR` | 其他 MySQL 錯誤 |

`mysqlrepl_monit` 區段設定：

| 鍵 | 預設 | 說明 |
|---|---|---|
| `enable` | `false` | 明確設為 `true` 才啟動 |
| `interval` | `300` | 檢查間隔秒數（120–86400） |
| `cycle` | `1` | 連續幾次 `REPLICATION_ERROR` 才發異常通知（1–100） |
| `recheck_delay` | `0` | 複製異常後二次確認的等待秒數；`0` 關閉，非 0 時夾在 20–180 |
| `account` | — | 連線帳號 |
| `pwd` | — | 連線密碼 |
| `ip` | `127.0.0.1` | 目標位址（空字串回退預設） |
| `port` | `3306` | 目標埠 |
| `connect_timeout` | `10` | 連線／讀寫逾時秒數（1–60） |

### 通知規則

- **異常**：連續 `cycle` 次 `REPLICATION_ERROR` 且本輪尚未發過，發送一次 `level=error`，標題「MySQL replication unusual」。
- **恢復**：先前處於異常，且本次結果為 `OK` 時，發送 `level=info`，標題「MySQL replication recovery」。
- **`MYSQL_DOWN` 不算複製失敗**：不累計 `cycle`、不發異常通知，也不會清除「異常中」狀態；MySQL 恢復但複製尚未恢復 `OK` 之前不會誤發恢復通知。
- **`AUTH_ERROR` / `NETWORK_ERROR` / `MYSQL_ERROR`**：只記 error 日誌，不發通知。

通知另附 IO/SQL 執行緒、延遲秒數、來源主機／埠／UUID、Last IO/SQL Error、GTID 集合等欄位（`MYSQL_DOWN`、`AUTH_ERROR`、`NETWORK_ERROR` 除外）。

```bash
<mail_command> send \
  --type notify \
  --level error|info \
  --subject "" \
  --title "MySQL replication unusual" \
  --flag oppmonit-repl-<檢查序號> \
  --caller op-watch \
  --sender "OpWatch(Go)" \
  --message "<詳細訊息>" \
  --row "Status,<狀態>" --row "IO_Running,<值>" --row "SQL_Running,<值>" ...
```

## 完整配置示例

```json
{
  // 通用
  "enable": true,
  "machine_updelay": 20,
  "enable_noti": true,
  "mail_command": "/usr/local/bin/openmailer",

  // 日誌（path 為檔案完整路徑；留空則只輸出到 stdout）
  "log": {
    "path": "/var/log/opwatch/opwatch.log",
    "level": "INFO",
    "maxsize": 10,
    "maxnum": 5
  },

  // 進程監控
  "process_monit": {
    "interval": 30,
    "ck_timeout": 10,
    "cycle": 3,
    "monitors": [
      { "name": "nginx",  "pattern": "nginx: master" },
      { "name": "mysqld", "pattern": "mysqld" }
    ]
  },

  // 磁碟監控
  "disk_monit": {
    "interval": 120,
    "ck_timeout": 10,
    "cycle": 2,
    "monitors": [
      { "name": "root", "path": "/", "exceed_threshold": 85, "recover_threshold": 75 },
      { "name": "data", "path": "/data", "exceed_threshold": 90, "recover_threshold": 80 }
    ]
  },

  // 記憶體監控（不需要時保留區段並設 enable=false）
  "mem_monit": {
    "enable": true,
    "warn_threshold": 80,
    "recover_threshold": 75,
    "interval": 60,
    "recheck_interval": 10,
    "recheck_cnt": 3
  },

  // MySQL 存活監控
  "mysqlalive_monit": {
    "enable": true,
    "interval": 30,
    "recheck_interval": 10,
    "recheck_cnt": 3,
    "port": 3306
  },

  // MySQL 複製監控
  "mysqlrepl_monit": {
    "enable": true,
    "interval": 300,
    "cycle": 1,
    "recheck_delay": 30,
    "account": "monitor",
    "pwd": "your-password",
    "ip": "127.0.0.1",
    "port": 3306,
    "connect_timeout": 10
  }
}
```

各監控的 `enable` 語義不同：`process_monit` / `disk_monit` 沒有 `enable` 開關，只要區段存在且 `monitors` 非空就啟動；`mem_monit` / `mysqlalive_monit` / `mysqlrepl_monit` 必須明設 `"enable": true`。