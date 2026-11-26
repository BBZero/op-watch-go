package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var Log *zap.SugaredLogger

type Config struct {
	Path    string // 日志文件路径，空字符串表示只输出到 Stdout
	Level   string // DEBUG, INFO, WARN, ERROR
	MaxSize int    // 单个日志文件最大 MB
	MaxNum  int    // 保留旧日志文件数
}

func DefaultConfig() *Config {
	return &Config{
		Path:    "",
		Level:   "INFO",
		MaxSize: 10,
		MaxNum:  5,
	}
}

func Init(cfg *Config) error {
	// 不区分大小写
	level := zapcore.InfoLevel
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = zapcore.DebugLevel
	case "info":
		level = zapcore.InfoLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	}

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:          "time",
		LevelKey:         "level",
		NameKey:          "logger",
		CallerKey:        "caller",
		MessageKey:       "msg",
		StacktraceKey:    "stacktrace",
		LineEnding:       zapcore.DefaultLineEnding,
		ConsoleSeparator: " ",
		EncodeLevel: func(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(fmt.Sprintf("[%-5s]", level.CapitalString()))
			// enc.AppendString(fmt.Sprintf("[%s] %-*s", level.CapitalString(), 5-len(level.CapitalString()), ""))
		}, //zapcore.CapitalLevelEncoder,
		EncodeTime: func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(t.Format("2006-01-02 15:04:05.000"))
		}, //zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	var writeSyncer zapcore.WriteSyncer

	if cfg.Path != "" {

		if err := os.MkdirAll(filepath.Dir(cfg.Path), 0755); err != nil {
			return fmt.Errorf("failed to create log directory: %w", err)
		}

		if _, err := os.Stat(cfg.Path); os.IsNotExist(err) {
			if f, err := os.OpenFile(cfg.Path, os.O_CREATE|os.O_WRONLY, 0644); err == nil {
				f.Close()
			}
		} else {
			os.Chmod(cfg.Path, 0644)
		}

		lumberjackLogger := &lumberjack.Logger{
			Filename:   cfg.Path,
			MaxSize:    cfg.MaxSize, // MB
			MaxBackups: cfg.MaxNum,  // 保留文件数
			MaxAge:     0,           // 保留 天
			Compress:   false,       // 压缩旧日志
		}

		writeSyncer = zapcore.NewMultiWriteSyncer(
			zapcore.AddSync(lumberjackLogger),
			zapcore.AddSync(os.Stdout),
		)
	} else {
		writeSyncer = zapcore.AddSync(os.Stdout)
	}

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		writeSyncer,
		level,
	)

	//zap.AddCaller() fatal will print stack
	// logger := zap.New(core, zap.AddStacktrace(zapcore.FatalLevel))
	logger := zap.New(core, zap.AddStacktrace(zapcore.FatalLevel), zap.AddCaller(), zap.AddCallerSkip(1))
	Log = logger.Sugar()

	return nil
}

func Sync() {
	if Log != nil {
		Log.Sync()
	}
}

func Debug(args ...interface{}) {
	if Log != nil {
		Log.Debug(args...)
	}
}

func Info(args ...interface{}) {
	if Log != nil {
		Log.Info(args...)
	}
}

func Warn(args ...interface{}) {
	if Log != nil {
		Log.Warn(args...)
	}
}

func Error(args ...interface{}) {
	if Log != nil {
		Log.Error(args...)
	}
}

func Fatal(args ...interface{}) {
	if Log != nil {
		Log.Fatal(args...)
	}
}

func Debugf(template string, args ...interface{}) {
	if Log != nil {
		Log.Debugf(template, args...)
	}
}

func Infof(template string, args ...interface{}) {
	if Log != nil {
		Log.Infof(template, args...)
	}
}

func Warnf(template string, args ...interface{}) {
	if Log != nil {
		Log.Warnf(template, args...)
	}
}

func Errorf(template string, args ...interface{}) {
	if Log != nil {
		Log.Errorf(template, args...)
	}
}

func Fatalf(template string, args ...interface{}) {
	if Log != nil {
		Log.Fatalf(template, args...)
	}
}
