// Package logger is used to store details of events in the node.
// Events can be categorized by Trace, Debug, Info, Error, Fatal, and Panic.
package logger

import (
	"log"
	"reflect"
	"runtime"

	"gorm.io/gorm"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger is the main interface of this package.
// It implements uber/zap's SugaredLogger interface and adds conditional logging helpers.
type Logger struct {
	*zap.SugaredLogger
	Orm         ORM
	lvl         zapcore.Level
	dir         string
	jsonConsole bool
	toDisk      bool
	fields      []interface{}
}

// Constants for service names for package specific logging configuration
const (
	HeadTracker = "head_tracker"
	FluxMonitor = "fluxmonitor"
	Keeper      = "keeper"
)

func GetLogServices() []string {
	return []string{
		HeadTracker,
		FluxMonitor,
		Keeper,
	}
}

// Write logs a message at the Info level and returns the length
// of the given bytes.
func (l *Logger) Write(b []byte) (int, error) {
	l.Info(string(b))
	return len(b), nil
}

// With creates a new logger with the given arguments
func (l *Logger) With(args ...interface{}) *Logger {
	newLogger := *l
	newLogger.SugaredLogger = l.SugaredLogger.With(args...)
	newLogger.fields = copyFields(l.fields, args...)
	return &newLogger
}

// copyFields returns a copy of fields with add appended.
func copyFields(fields []interface{}, add ...interface{}) []interface{} {
	f := make([]interface{}, 0, len(fields)+len(add))
	f = append(f, fields...)
	f = append(f, add...)
	return f
}

// Named creates a new named logger with the given name
func (l *Logger) Named(name string) *Logger {
	newLogger := *l
	newLogger.SugaredLogger = l.SugaredLogger.Named(name).With("id", name)
	newLogger.fields = copyFields(l.fields, "id", name)
	return &newLogger
}

// WithError adds the given error to the log
func (l *Logger) WithError(err error) *Logger {
	return l.With("error", err)
}

// WarnIf logs the error if present.
func (l *Logger) WarnIf(err error) {
	if err != nil {
		l.Warn(err)
	}
}

// ErrorIf logs the error if present.
func (l *Logger) ErrorIf(err error, optionalMsg ...string) {
	if err != nil {
		if len(optionalMsg) > 0 {
			l.Error(errors.Wrap(err, optionalMsg[0]))
		} else {
			l.Error(err)
		}
	}
}

// ErrorIfCalling calls the given function and logs the error of it if there is.
func (l *Logger) ErrorIfCalling(f func() error, optionalMsg ...string) {
	err := f()
	if err != nil {
		e := errors.Wrap(err, runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name())
		if len(optionalMsg) > 0 {
			Default.Error(errors.Wrap(e, optionalMsg[0]))
		} else {
			Default.Error(e)
		}
	}
}

func (l *Logger) PanicIf(err error) {
	if err != nil {
		l.Panic(err)
	}
}

func (l *Logger) SetDB(db *gorm.DB) {
	l.Orm = NewORM(db)
}

// GetServiceLogLevels retrieves all service log levels from the db
func (l *Logger) GetServiceLogLevels() (map[string]string, error) {
	serviceLogLevels := make(map[string]string)

	for _, svcName := range GetLogServices() {
		svc, err := l.ServiceLogLevel(svcName)
		if err != nil {
			Fatalf("error getting service log levels: %v", err)
		}
		serviceLogLevels[svcName] = svc
	}

	return serviceLogLevels, nil
}

// CreateLogger creates a new Logger with the given SugaredLogger
func CreateLogger(zl *zap.SugaredLogger) *Logger {
	return &Logger{
		SugaredLogger: zl,
	}
}

// initLogConfig builds a zap.Config for a logger
func initLogConfig(dir string, jsonConsole bool, lvl zapcore.Level, toDisk bool) zap.Config {
	config := zap.NewProductionConfig()
	if !jsonConsole {
		config.OutputPaths = []string{"pretty://console"}
	}
	if toDisk {
		destination := logFileURI(dir)
		config.OutputPaths = append(config.OutputPaths, destination)
		config.ErrorOutputPaths = append(config.ErrorOutputPaths, destination)
	}
	config.Level.SetLevel(lvl)
	return config
}

// CreateProductionLogger returns a log config for the passed directory
// with the given LogLevel and customizes stdout for pretty printing.
func CreateProductionLogger(
	dir string, jsonConsole bool, lvl zapcore.Level, toDisk bool) *Logger {
	config := initLogConfig(dir, jsonConsole, lvl, toDisk)

	zl, err := config.Build(zap.AddCallerSkip(1))
	if err != nil {
		log.Fatal(err)
	}
	return &Logger{
		SugaredLogger: zl.Sugar(),
		lvl:           lvl,
		dir:           dir,
		jsonConsole:   jsonConsole,
		toDisk:        toDisk,
	}
}

// InitServiceLevelLogger builds a service level logger with a given logging level & serviceName
func (l *Logger) InitServiceLevelLogger(serviceName string, logLevel string) (*Logger, error) {
	var ll zapcore.Level
	if err := ll.UnmarshalText([]byte(logLevel)); err != nil {
		return nil, err
	}

	config := initLogConfig(l.dir, l.jsonConsole, ll, l.toDisk)

	zl, err := config.Build(zap.AddCallerSkip(1))
	if err != nil {
		return nil, err
	}

	newLogger := *l
	newLogger.SugaredLogger = zl.Named(serviceName).Sugar().With(l.fields...)
	newLogger.fields = copyFields(l.fields)
	return &newLogger, nil
}

// ServiceLogLevel is the log level set for a specified package
func (l *Logger) ServiceLogLevel(serviceName string) (string, error) {
	if l.Orm != nil {
		level, err := l.Orm.GetServiceLogLevel(serviceName)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			Warnf("Error while trying to fetch %s service log level: %v", serviceName, err)
		} else if err == nil {
			return level, nil
		}
	}
	return l.lvl.String(), nil
}

// NewProductionConfig returns a production logging config
func NewProductionConfig(lvl zapcore.Level, dir string, jsonConsole, toDisk bool) (c zap.Config) {
	var outputPath string
	if jsonConsole {
		outputPath = "stderr"
	} else {
		outputPath = "pretty://console"
	}
	// Mostly copied from zap.NewProductionConfig with sampling disabled
	c = zap.Config{
		Level:            zap.NewAtomicLevelAt(lvl),
		Development:      false,
		Sampling:         nil,
		Encoding:         "json",
		EncoderConfig:    NewProductionEncoderConfig(),
		OutputPaths:      []string{outputPath},
		ErrorOutputPaths: []string{"stderr"},
	}
	if toDisk {
		destination := logFileURI(dir)
		c.OutputPaths = append(c.OutputPaths, destination)
		c.ErrorOutputPaths = append(c.ErrorOutputPaths, destination)
	}
	return
}

// NewProductionEncoderConfig returns a production encoder config
func NewProductionEncoderConfig() zapcore.EncoderConfig {
	// Copied from zap.NewProductionEncoderConfig but with ISO timestamps instead of Unix
	return zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
}
