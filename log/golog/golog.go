// The colorful and simple logging library
// Copyright (c) 2017 Fadhli Dzil Ikram

package golog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	terminal "golang.org/x/term"

	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/log/golog/colorful"
)

func init() {
	log.RegisterLogger(New(os.Stdout))
}

// FdWriter interface extends existing io.Writer with file descriptor function
// support
type FdWriter interface {
	io.Writer
	Fd() uintptr
}

// Logger struct define the underlying storage for single logger
type Logger struct {
	mu        sync.RWMutex
	writeMu   sync.Mutex
	color     bool
	out       io.Writer
	debug     bool
	timestamp bool
	quiet     bool
	logLevel  int32
	logChan   chan logEntry
}

// logEntry is a formatted line waiting to be flushed, or a barrier when line
// is nil
type logEntry struct {
	line  []byte
	flush chan struct{}
}

// logQueueSize is large enough to absorb a burst of per-connection logs
const logQueueSize = 4096

// Prefix struct define plain and color byte
type Prefix struct {
	Plain []byte
	Color []byte
	File  bool
}

var (
	// Plain prefix template
	plainFatal = []byte("[FATAL] ")
	plainError = []byte("[ERROR] ")
	plainWarn  = []byte("[WARN]  ")
	plainInfo  = []byte("[INFO]  ")
	plainDebug = []byte("[DEBUG] ")
	plainTrace = []byte("[TRACE] ")

	// FatalPrefix show fatal prefix
	FatalPrefix = Prefix{
		Plain: plainFatal,
		Color: colorful.Red(plainFatal),
		File:  true,
	}

	// ErrorPrefix show error prefix
	ErrorPrefix = Prefix{
		Plain: plainError,
		Color: colorful.Red(plainError),
		File:  true,
	}

	// WarnPrefix show warn prefix
	WarnPrefix = Prefix{
		Plain: plainWarn,
		Color: colorful.Orange(plainWarn),
	}

	// InfoPrefix show info prefix
	InfoPrefix = Prefix{
		Plain: plainInfo,
		Color: colorful.Green(plainInfo),
	}

	// DebugPrefix show info prefix
	DebugPrefix = Prefix{
		Plain: plainDebug,
		Color: colorful.Purple(plainDebug),
		File:  true,
	}

	// TracePrefix show info prefix
	TracePrefix = Prefix{
		Plain: plainTrace,
		Color: colorful.Cyan(plainTrace),
	}
)

// New returns new Logger instance with predefined writer output and
// automatically detect terminal coloring support
func New(out FdWriter) *Logger {
	logger := &Logger{
		color:     terminal.IsTerminal(int(out.Fd())),
		out:       out,
		timestamp: true,
		logChan:   make(chan logEntry, logQueueSize),
	}
	go logger.writeLoop()
	return logger
}

// writeLoop is the only goroutine touching the output writer, so a slow
// terminal never blocks the goroutines that emit logs
func (l *Logger) writeLoop() {
	for entry := range l.logChan {
		if entry.line != nil {
			l.writeLine(entry.line)
		}
		if entry.flush != nil {
			close(entry.flush)
		}
	}
}

// writeLine serializes writes between the writer goroutine and a synchronous
// message written right before exit
func (l *Logger) writeLine(line []byte) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	l.mu.RLock()
	out := l.out
	l.mu.RUnlock()
	_, _ = out.Write(line)
}

// Flush blocks until every queued line has been written out
func (l *Logger) Flush() {
	done := make(chan struct{})
	l.logChan <- logEntry{flush: done}
	<-done
}

func (l *Logger) SetLogLevel(level log.LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	atomic.StoreInt32(&l.logLevel, int32(level))
}

func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.color = false
	if fdw, ok := w.(FdWriter); ok {
		l.color = terminal.IsTerminal(int(fdw.Fd()))
	}
	l.out = w
}

// WithColor explicitly turn on colorful features on the log
func (l *Logger) WithColor() *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.color = true
	return l
}

// WithoutColor explicitly turn off colorful features on the log
func (l *Logger) WithoutColor() *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.color = false
	return l
}

// WithDebug turn on debugging output on the log to reveal debug and trace level
func (l *Logger) WithDebug() *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.debug = true
	return l
}

// WithoutDebug turn off debugging output on the log
func (l *Logger) WithoutDebug() *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.debug = false
	return l
}

// IsDebug check the state of debugging output
func (l *Logger) IsDebug() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.debug
}

// WithTimestamp turn on timestamp output on the log
func (l *Logger) WithTimestamp() *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.timestamp = true
	return l
}

// WithoutTimestamp turn off timestamp output on the log
func (l *Logger) WithoutTimestamp() *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.timestamp = false
	return l
}

// Quiet turn off all log output
func (l *Logger) Quiet() *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.quiet = true
	return l
}

// NoQuiet turn on all log output
func (l *Logger) NoQuiet() *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.quiet = false
	return l
}

// IsQuiet check for quiet state
func (l *Logger) IsQuiet() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.quiet
}

// Output print the actual value
func (l *Logger) Output(depth int, prefix Prefix, data string) error {
	// one more frame is on the stack here (Output itself), so the depth has to
	// be compensated to keep the reported caller the same as before
	l.output(depth+1, prefix, data, false)
	return nil
}

// output enqueues a formatted line. Important lines (errors, fatal messages)
// wait for the writer when the queue is full instead of being dropped.
func (l *Logger) output(depth int, prefix Prefix, data string, important bool) {
	line := l.format(depth+1, prefix, data)
	if line == nil {
		return
	}
	select {
	case l.logChan <- logEntry{line: line}:
		return
	default:
	}
	if !important {
		// the output cannot keep up, drop the line instead of blocking a relay
		// goroutine on a slow terminal
		return
	}
	select {
	case l.logChan <- logEntry{line: line}:
	case <-time.After(time.Second):
	}
}

// outputSync writes a line immediately, bypassing the async queue. It is used
// for messages that must not be lost before the process exits.
func (l *Logger) outputSync(depth int, prefix Prefix, data string) {
	line := l.format(depth+1, prefix, data)
	if line == nil {
		return
	}
	l.writeLine(line)
}

func (l *Logger) format(depth int, prefix Prefix, data string) []byte {
	// Check if quiet is requested, and try to return nil and be quiet
	if l.IsQuiet() {
		return nil
	}
	// Get current time
	now := time.Now()
	// Temporary storage for file and line tracing
	var file string
	var line int
	var fn string
	// Check if the specified prefix needs to be included with file logging
	if prefix.File {
		var ok bool
		var pc uintptr

		// Get the caller filename and line
		if pc, file, line, ok = runtime.Caller(depth + 2); !ok {
			file = "<unknown file>"
			fn = "<unknown function>"
			line = 0
		} else {
			file = filepath.Base(file)
			fn = runtime.FuncForPC(pc).Name()
		}
	}
	l.mu.RLock()
	color := l.color
	timestamp := l.timestamp
	l.mu.RUnlock()

	// Build the line in a local buffer so formatting never contends with the
	// writer goroutine
	buf := colorful.ColorBuffer{}
	// Write prefix to the buffer
	if color {
		buf.Append(prefix.Color)
	} else {
		buf.Append(prefix.Plain)
	}
	// Check if the log require timestamping
	if timestamp {
		if color {
			buf.Blue()
		}
		// Print date and time
		year, month, day := now.Date()
		buf.AppendInt(year, 4)
		buf.AppendByte('/')
		buf.AppendInt(int(month), 2)
		buf.AppendByte('/')
		buf.AppendInt(day, 2)
		buf.AppendByte(' ')
		hour, min, sec := now.Clock()
		buf.AppendInt(hour, 2)
		buf.AppendByte(':')
		buf.AppendInt(min, 2)
		buf.AppendByte(':')
		buf.AppendInt(sec, 2)
		buf.AppendByte(' ')
		// Print reset color if color enabled
		if color {
			buf.Off()
		}
	}
	// Add caller filename and line if enabled
	if prefix.File {
		// Print color start if enabled
		if color {
			buf.Orange()
		}
		// Print filename and line
		buf.Append([]byte(fn))
		buf.AppendByte(':')
		buf.Append([]byte(file))
		buf.AppendByte(':')
		buf.AppendInt(line, 0)
		buf.AppendByte(' ')
		// Print color stop
		if color {
			buf.Off()
		}
	}
	// Print the actual string data from caller
	buf.Append([]byte(data))
	if len(data) == 0 || data[len(data)-1] != '\n' {
		buf.AppendByte('\n')
	}

	lineBytes := make([]byte, len(buf.Buffer))
	copy(lineBytes, buf.Buffer)
	return lineBytes
}

// Fatal print fatal message to output and quit the application with status 1
func (l *Logger) Fatal(v ...interface{}) {
	if atomic.LoadInt32(&l.logLevel) <= 4 {
		// drain pending lines first, then write the fatal one synchronously so
		// it cannot be dropped by a full queue
		l.Flush()
		l.outputSync(1, FatalPrefix, fmt.Sprintln(v...))
	}
	os.Exit(1)
}

// Fatalf print formatted fatal message to output and quit the application
// with status 1
func (l *Logger) Fatalf(format string, v ...interface{}) {
	if atomic.LoadInt32(&l.logLevel) <= 4 {
		l.Flush()
		l.outputSync(1, FatalPrefix, fmt.Sprintf(format, v...))
	}
	os.Exit(1)
}

// Error print error message to output
func (l *Logger) Error(v ...interface{}) {
	if atomic.LoadInt32(&l.logLevel) <= 3 {
		l.output(1, ErrorPrefix, fmt.Sprintln(v...), true)
	}
}

// Errorf print formatted error message to output
func (l *Logger) Errorf(format string, v ...interface{}) {
	if atomic.LoadInt32(&l.logLevel) <= 3 {
		l.output(1, ErrorPrefix, fmt.Sprintf(format, v...), true)
	}
}

// Warn print warning message to output
func (l *Logger) Warn(v ...interface{}) {
	if atomic.LoadInt32(&l.logLevel) <= 2 {
		l.Output(1, WarnPrefix, fmt.Sprintln(v...))
	}
}

// Warnf print formatted warning message to output
func (l *Logger) Warnf(format string, v ...interface{}) {
	if atomic.LoadInt32(&l.logLevel) <= 2 {
		l.Output(1, WarnPrefix, fmt.Sprintf(format, v...))
	}
}

// Info print informational message to output
func (l *Logger) Info(v ...interface{}) {
	if atomic.LoadInt32(&l.logLevel) <= 1 {
		l.Output(1, InfoPrefix, fmt.Sprintln(v...))
	}
}

// Infof print formatted informational message to output
func (l *Logger) Infof(format string, v ...interface{}) {
	if atomic.LoadInt32(&l.logLevel) <= 1 {
		l.Output(1, InfoPrefix, fmt.Sprintf(format, v...))
	}
}

// Debug print debug message to output if debug output enabled
func (l *Logger) Debug(v ...interface{}) {
	if atomic.LoadInt32(&l.logLevel) == 0 {
		l.Output(1, DebugPrefix, fmt.Sprintln(v...))
	}
}

// Debugf print formatted debug message to output if debug output enabled
func (l *Logger) Debugf(format string, v ...interface{}) {
	if atomic.LoadInt32(&l.logLevel) == 0 {
		l.Output(1, DebugPrefix, fmt.Sprintf(format, v...))
	}
}

// Trace print trace message to output if debug output enabled
func (l *Logger) Trace(v ...interface{}) {
	if atomic.LoadInt32(&l.logLevel) == 0 {
		l.Output(1, TracePrefix, fmt.Sprintln(v...))
	}
}

// Tracef print formatted trace message to output if debug output enabled
func (l *Logger) Tracef(format string, v ...interface{}) {
	if atomic.LoadInt32(&l.logLevel) == 0 {
		l.Output(1, TracePrefix, fmt.Sprintf(format, v...))
	}
}
