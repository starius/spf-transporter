package log

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"runtime/debug"
	"strings"
	"sync"
)

type (
	// Logger is a wrapper for the standard library logger that enforces logging
	// with the provided settings. It also supports a Close method, which
	// attempts to close the underlying io.Writer.
	Logger struct {
		*log.Logger
		staticW       io.Writer
		staticOptions Options
	}

	// Options contains logger options. It is required to instantiate the
	// logger.
	Options struct {
		// BinaryName is the name of the binary.
		BinaryName string
		// BugReportURL contains the URL where bug reports should be submitted.
		BugReportURL string
		// Debug enables debug logging and will cause the logger to panic when
		// calling Critical or Severe.
		Debug bool
		// Release is the release mode.
		Release ReleaseType
		// TagManager is an entity that defines which tags are enabled, and thus
		// decides when a tag-enabled logger effectively prints the given
		// message.
		TagManager TagManager
		// Version is the binary version.
		Version string
	}

	// ReleaseType is the type of the release.
	ReleaseType uint

	// TagLogger defines a set of methods that allow conditional logging. The
	// logger will only print when it has at least one tag that is enabled in
	// the tag manager. Every method excepts an extra set of tags, that simply
	// extend the tag logger's current set tag set.
	TagLogger interface {
		Debugln(msg interface{}, tags ...string)
		Println(msg interface{}, tags ...string)
		PrintWithTags(msg interface{}, tags ...string)
		WithTags(tags ...string) TagLogger
	}

	// tagLogger wraps a parent logger and extends it with its own set of tags.
	tagLogger struct {
		staticParent *Logger
		staticTags   []string
	}
)

const (
	// ReleaseTypeError is an uninitialized ReleaseType.
	ReleaseTypeError ReleaseType = iota
	// Release is the release type used for production builds.
	Release
	// Dev is the release type used for dev builds.
	Dev
	// Testing is the release type used for testing builds.
	Testing
)

var (
	// DiscardLogger is a logger that writes to ioutil.Discard. It is only meant
	// to be used for testing and will panic on Critical.
	DiscardLogger = newDiscardLogger()
)

// String returns a string representation of the ReleaseType.
func (rt ReleaseType) String() string {
	switch rt {
	case ReleaseTypeError:
		// Developer error. Panic as we can't call Critical here.
		panic("uninitialized release type")
	case Release:
		return "release"
	case Dev:
		return "dev"
	case Testing:
		return "testing"
	default:
		// Developer error. Panic as we can't call Critical here.
		panic("unknown release type")
	}
}

// BuildInfoString is used to include information about the current build when
// Critical or Severe are called.
func (o *Options) BuildInfoString() string {
	return fmt.Sprintf("(%v v%v, Release: %s)", o.BinaryName, o.Version, o.Release)
}

// Critical should be called if a sanity check has failed, indicating developer
// error. Critical is called with an extended message guiding the user to the
// issue tracker on Github. If the program does not panic, the call stack for
// the running goroutine is printed to help determine the error.
func (o *Options) Critical(v ...interface{}) {
	s := fmt.Sprintf("Critical error: %v %vPlease submit a bug report here: %v\n", o.BuildInfoString(), fmt.Sprintln(v...), o.BugReportURL)
	if o.Release != Testing {
		debug.PrintStack()
		_, _ = os.Stderr.WriteString(s)
	}
	if o.Debug {
		panic(s)
	}
}

// BuildInfoString is used to include information about the current build when
// Critical or Severe are called.
func (l *Logger) BuildInfoString() string {
	return l.staticOptions.BuildInfoString()
}

// Close logs a shutdown message and closes the Logger's underlying io.Writer,
// if it is also an io.Closer.
func (l *Logger) Close() error {
	err := l.Output(2, "SHUTDOWN: Logging has terminated.")
	if c, ok := l.staticW.(io.Closer); ok {
		return c.Close()
	}
	return err
}

// Critical logs a message with a CRITICAL prefix that guides the user to the
// github tracker. If debug mode is enabled, it will also write the message to
// os.Stderr and panic. Critical should only be called if there has been a
// developer error, otherwise Severe should be called.
func (l *Logger) Critical(v ...interface{}) {
	_ = l.Output(2, "CRITICAL: "+fmt.Sprintln(v...))
	l.staticOptions.Critical(v...)
}

// Debug is equivalent to Logger.Print when build.DEBUG is true. Otherwise it
// is a no-op.
func (l *Logger) Debug(v ...interface{}) {
	if l.staticOptions.Debug {
		_ = l.Output(2, fmt.Sprint(v...))
	}
}

// Debugf is equivalent to Logger.Printf when build.DEBUG is true. Otherwise it
// is a no-op.
func (l *Logger) Debugf(format string, v ...interface{}) {
	if l.staticOptions.Debug {
		_ = l.Output(2, fmt.Sprintf(format, v...))
	}
}

// Debugln is equivalent to Logger.Println when build.DEBUG is true. Otherwise
// it is a no-op.
func (l *Logger) Debugln(v ...interface{}) {
	if l.staticOptions.Debug {
		_ = l.Output(2, "[DEBUG] "+fmt.Sprintln(v...))
	}
}

// Errorf is equivalent to Logger.Printf with '[ERROR] ' prefix.
func (l *Logger) Errorf(format string, v ...interface{}) {
	_ = l.Output(2, "[ERROR] "+fmt.Sprintf(format, v...))
}

// Errorln is equivalent to Logger.Println with '[ERROR] ' prefix.
func (l *Logger) Errorln(v ...interface{}) {
	_ = l.Output(2, "[ERROR] "+fmt.Sprintln(v...))
}

// PrintWithTags calls Println conditionally, it will only print if at least one
// of the given tags is enabled by the tag manager.
func (l *Logger) PrintWithTags(msg interface{}, tags ...string) {
	ok, prefix := l.shouldPrint(tags)
	if !ok {
		return
	}
	if prefix != "" {
		msg = fmt.Sprintf("%s: %s", prefix, msg)
	}
	l.Println(msg)
}

// Severe logs a message with a SEVERE prefix. If debug mode is enabled, it
// will also write the message to os.Stderr and panic. Severe should be called
// if there is a severe problem with the user's machine or setup that should be
// addressed ASAP but does not necessarily require that the machine crash or
// exit.
func (l *Logger) Severe(v ...interface{}) {
	_ = l.Output(2, "SEVERE: "+fmt.Sprintln(v...))
	s := fmt.Sprintf("Severe error: %v %v", l.BuildInfoString(), fmt.Sprintln(v...))
	if l.staticOptions.Release != Testing {
		debug.PrintStack()
		_, _ = os.Stderr.WriteString(s)
	}
	if l.staticOptions.Debug {
		panic(s)
	}
}

// WithTags wraps the logger and extends its set of tags with the given tags.
func (l *Logger) WithTags(tags ...string) TagLogger {
	return &tagLogger{l, tags}
}

// Debugln calls Debugln on the parent logger, but appends its own tags.
func (tl *tagLogger) Debugln(msg interface{}, tags ...string) {
	l := tl.staticParent
	ok, prefix := l.shouldPrint(append(tl.staticTags, tags...))
	if !ok {
		return
	}

	if prefix != "" {
		msg = fmt.Sprintf("%s: %s", prefix, msg)
	}
	l.Debugln(msg)
}

// Println calls Println on the parent logger, but appends its own tags.
func (tl *tagLogger) Println(msg interface{}, tags ...string) {
	l := tl.staticParent
	ok, prefix := l.shouldPrint(append(tl.staticTags, tags...))
	if !ok {
		return
	}

	if prefix != "" {
		msg = fmt.Sprintf("%s: %s", prefix, msg)
	}
	l.Println(msg)
}

// PrintWithTags calls Println internally, we duplicate this method as
// PrintWithTags because it's defined on the logger as well.
func (tl *tagLogger) PrintWithTags(msg interface{}, tags ...string) {
	tl.Println(msg, tags...)
}

// WithTags returns a tag logger that extends its own set of tags with the given
// tags.
func (tl *tagLogger) WithTags(tags ...string) TagLogger {
	return &tagLogger{tl.staticParent, append(tl.staticTags, tags...)}
}

// shouldPrint returns whether or not the logger should print the message
// according to its set of tags
func (l *Logger) shouldPrint(tags []string) (bool, string) {
	// print if no tags are passed
	if len(tags) == 0 {
		return true, ""
	}

	// filter out all enabled tags
	var enabled []string

	// if no tag manager is set, all tags are enabled by default
	if l.staticOptions.TagManager == nil {
		enabled = tags
	} else {
		lookup := make(map[string]struct{})
		for _, tag := range tags {
			_, exists := lookup[tag]
			if l.staticOptions.TagManager.HasTag(tag) && !exists {
				lookup[tag] = struct{}{}
				enabled = append(enabled, tag)
			}
		}
	}

	// print if at least one tag is enabled
	if len(enabled) > 0 {
		var parts []string
		for _, tag := range enabled {
			parts = append(parts, tag)
		}
		return true, strings.Join(parts, " | ")
	}

	return false, ""
}

// NewLogger returns a logger that can be closed. Calls should not be made to
// the logger after 'Close' has been called.
func NewLogger(w io.Writer, options Options) (*Logger, error) {
	switch options.Release {
	case Release, Dev, Testing:
	default:
		return nil, fmt.Errorf("invalid ReleaseType provided: %v", options.Release.String())
	}

	// use global tag manager if none was defined
	if options.TagManager == nil {
		options.TagManager = GlobalTagManager
	}

	message := fmt.Sprintf("STARTUP: Logging has started. %v Version %v", options.BinaryName, options.Version)
	l := log.New(w, "", log.Ldate|log.Ltime|log.Lmicroseconds|log.Lshortfile|log.LUTC)
	err := l.Output(3, message) // Call depth is 3 because NewLogger is usually called by NewFileLogger
	if err != nil {
		return nil, err
	}

	return &Logger{l, w, options}, nil
}

// closeableFile wraps an os.File to perform sanity checks on its Write and
// Close methods. When the checks are enabled, calls to Write or Close will
// panic if they are called after the file has already been closed.
type closeableFile struct {
	*os.File
	closed        bool
	staticOptions Options

	mu sync.RWMutex
}

// Close closes the file and sets the closed flag.
func (cf *closeableFile) Close() error {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	// Sanity check - close should not have been called yet.
	if cf.closed {
		cf.staticOptions.Critical("cannot close the file; already closed")
	}

	// Ensure that all data has actually hit the disk.
	if err := cf.File.Sync(); err != nil {
		return err
	}
	cf.closed = true
	return cf.File.Close()
}

// Write takes the input data and writes it to the file.
func (cf *closeableFile) Write(b []byte) (int, error) {
	cf.mu.RLock()
	defer cf.mu.RUnlock()
	// Sanity check - close should not have been called yet.
	if cf.closed {
		cf.staticOptions.Critical("cannot write to the file after it has been closed")
	}
	return cf.File.Write(b)
}

// NewFileLogger returns a logger that logs to logFilename. The file is opened
// in append mode, and created if it does not exist.
func NewFileLogger(logFilename string, options Options) (*Logger, error) {
	logFile, err := os.OpenFile(logFilename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0660)
	if err != nil {
		return nil, err
	}
	cf := &closeableFile{File: logFile, staticOptions: options}
	return NewLogger(cf, options)
}

// newDiscardLogger returns a new logger that writes to ioutil.Discard.
func newDiscardLogger() *Logger {
	w := ioutil.Discard
	l := log.New(w, "", 0)
	options := Options{
		BinaryName: "discard",
		// Set Debug to panic on Critical.
		Debug: true,
		// Set the release type to avoid "uninitialized release type" panic. The
		// discard logger is mostly used for testing.
		Release: Testing,
		// Set TagManager to the GlobalTagManager.
		TagManager: GlobalTagManager,
		Version:    "0",
	}

	return &Logger{l, w, options}
}
