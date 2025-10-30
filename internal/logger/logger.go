package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Config controls logger behaviour.
type Config struct {
	Directory    string
	MirrorStdout bool
}

var (
	baseDir      = "logs"
	mirrorStdout = true
	once         sync.Once
	configured   bool
	mu           sync.Mutex
	loggers      sync.Map
)

// Init configures the logging system. It is safe to call multiple times.
func Init(cfg Config) error {
	var initErr error
	once.Do(func() {
		configured = true
		if strings.TrimSpace(cfg.Directory) != "" {
			baseDir = cfg.Directory
		}
		if err := os.MkdirAll(baseDir, 0o755); err != nil {
			initErr = err
			return
		}
		mirrorStdout = cfg.MirrorStdout
	})
	return initErr
}

// SetMirrorStdout allows toggling stdout mirroring after initialization.
func SetMirrorStdout(enabled bool) {
	mu.Lock()
	defer mu.Unlock()
	mirrorStdout = enabled
	loggers.Range(func(key, value any) bool {
		logger := value.(*ModuleLogger)
		logger.resetWriters()
		return true
	})
}

// Get returns a logger for the given module, creating it if necessary.
func Get(module string) *ModuleLogger {
	if module == "" {
		module = "default"
	}
	if value, ok := loggers.Load(module); ok {
		return value.(*ModuleLogger)
	}

	mu.Lock()
	defer mu.Unlock()

	if value, ok := loggers.Load(module); ok {
		return value.(*ModuleLogger)
	}

	if !configured {
		_ = Init(Config{Directory: baseDir, MirrorStdout: mirrorStdout})
	}

	filePath := filepath.Join(baseDir, module+".log")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		panic(err)
	}

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		panic(err)
	}

	var writer io.Writer = file
	if mirrorStdout {
		writer = io.MultiWriter(file, os.Stdout)
	}

	logger := &ModuleLogger{module: module, writer: writer, file: file}
	loggers.Store(module, logger)
	return logger
}

// ModuleLogger renders human-readable structured lines。
type ModuleLogger struct {
	module string
	writer io.Writer
	file   *os.File
	mu     sync.Mutex
}

func (l *ModuleLogger) resetWriters() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return
	}
	if mirrorStdout {
		l.writer = io.MultiWriter(l.file, os.Stdout)
	} else {
		l.writer = l.file
	}
}

func (l *ModuleLogger) Printf(format string, args ...interface{}) {
	l.write("INFO", fmt.Sprintf(format, args...))
}

func (l *ModuleLogger) Println(args ...interface{}) {
	l.write("INFO", fmt.Sprintln(args...))
}

func (l *ModuleLogger) Fatal(args ...interface{}) {
	l.write("FATAL", fmt.Sprint(args...))
	os.Exit(1)
}

func (l *ModuleLogger) Fatalf(format string, args ...interface{}) {
	l.write("FATAL", fmt.Sprintf(format, args...))
	os.Exit(1)
}

func (l *ModuleLogger) write(level, message string) {
	ts := time.Now().Format(time.RFC3339Nano)
	msg := strings.TrimRight(message, "\n")
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.writer, "%s %-5s [%s] %s\n", ts, level, l.module, msg)
}
