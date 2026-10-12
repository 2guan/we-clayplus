package plugin

import (
	"bytes"
	"context"
	"io"
	"log"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/go-hclog"
)

type slogWrapper struct {
	slog    *slog.Logger
	oriSlog *slog.Logger
	lvar    *slog.LevelVar
	names   []string
	args    []interface{}
}

func (s *slogWrapper) clone() *slogWrapper {
	newSlog := *s.slog
	var oriSlog *slog.Logger = nil
	if s.oriSlog != nil {
		newOriSlog := *s.oriSlog
		oriSlog = &newOriSlog
	}
	return &slogWrapper{
		slog:    &newSlog,
		oriSlog: oriSlog,
		names:   append([]string{}, s.names...),
		args:    append([]interface{}{}, s.args...),
	}
}

var _ hclog.Logger = &slogWrapper{}

const (
	SlogLevelTrace = slog.LevelDebug - 4
	SlogLevelOff   = slog.LevelError + 4
)

var levelMapToSlog = map[hclog.Level]slog.Level{
	hclog.Off:   SlogLevelOff,
	hclog.Error: slog.LevelError,
	hclog.Warn:  slog.LevelWarn,
	hclog.Info:  slog.LevelInfo,
	hclog.Debug: slog.LevelDebug,
	hclog.Trace: SlogLevelTrace,
}

var levelMapFromSlog = map[slog.Level]hclog.Level{
	SlogLevelOff:    hclog.Off,
	slog.LevelError: hclog.Error,
	slog.LevelWarn:  hclog.Warn,
	slog.LevelInfo:  hclog.Info,
	slog.LevelDebug: hclog.Debug,
	SlogLevelTrace:  hclog.Trace,
}

// New wraps a slog.Logger to a hclog.Logger.
// The `SetLevel` method only works when the `lvar` is specified and set to the slog.Logger.
func newLogger(l *slog.Logger, lvar *slog.LevelVar) hclog.Logger {
	return &slogWrapper{
		slog:    l,
		oriSlog: nil,
		lvar:    lvar,
		names:   []string{},
		args:    []interface{}{},
	}
}

func (s *slogWrapper) handle(msg string, args []interface{}) (string, []interface{}) {
	for _, arg := range args {
		if arg == "timestamp" {
			args = args[0 : len(args)-2]
		}
	}
	var prefix string
	if s.Name() != "" {
		prefix = s.Name() + ": "
	}
	return prefix + msg, args
}

func (s *slogWrapper) Trace(msg string, args ...interface{}) {
	msg, args = s.handle(msg, args)
	s.slog.Log(context.Background(), SlogLevelTrace, msg, args...)
}

func (s *slogWrapper) Debug(msg string, args ...interface{}) {
	msg, args = s.handle(msg, args)
	s.slog.Debug(msg, args...)
}

func (s *slogWrapper) Info(msg string, args ...interface{}) {
	msg, args = s.handle(msg, args)
	s.slog.Info(msg, args...)
}

func (s *slogWrapper) Warn(msg string, args ...interface{}) {
	msg, args = s.handle(msg, args)
	s.slog.Warn(msg, args...)
}

func (s *slogWrapper) Error(msg string, args ...interface{}) {
	msg, args = s.handle(msg, args)
	s.slog.Error(msg, args...)
}

func (s *slogWrapper) GetLevel() hclog.Level {
	if s.lvar == nil {
		// lvar not set indicates the source slog.Logger has a fixed log level (or a default level, which equals to Info).
		// In this case, we enumerate the log levels from lowest (Trace) to get the effective log level.
		return s.getLowestLevel()
	}
	return levelMapFromSlog[s.lvar.Level()]
}

// SetLevel only applies when the source slog.Logger has a slog.LevelVar level.
func (s *slogWrapper) SetLevel(level hclog.Level) {
	if s.lvar != nil {
		s.lvar.Set(levelMapToSlog[level])
	}
}

func (s *slogWrapper) IsTrace() bool {
	return s.slog.Enabled(context.Background(), SlogLevelTrace)
}

func (s *slogWrapper) IsDebug() bool {
	return s.slog.Enabled(context.Background(), slog.LevelDebug)
}

func (s *slogWrapper) IsInfo() bool {
	return s.slog.Enabled(context.Background(), slog.LevelInfo)
}

func (s *slogWrapper) IsWarn() bool {
	return s.slog.Enabled(context.Background(), slog.LevelWarn)
}

func (s *slogWrapper) IsError() bool {
	return s.slog.Enabled(context.Background(), slog.LevelError)
}

func (s *slogWrapper) Log(level hclog.Level, msg string, args ...interface{}) {
	s.slog.Log(context.Background(), levelMapToSlog[level], msg, args...)
}

func (s *slogWrapper) Name() string {
	return strings.Join(s.names, ".")
}

func (s *slogWrapper) Named(name string) hclog.Logger {
	sl := s.clone()
	if len(s.names) == 0 {
		newSlog := *sl.slog
		sl.oriSlog = &newSlog
	}
	sl.names = append(sl.names, name)
	//sl.slog = s.slog.WithGroup(name)
	return sl
}

func (s *slogWrapper) ResetNamed(name string) hclog.Logger {
	sl := s.clone()

	// Empty name indicates to clear the name
	if name == "" {
		if len(sl.names) == 0 {
			return sl
		}
		sl.names = []string{}
		sl.slog = sl.oriSlog
		sl.oriSlog = nil
		return sl
	}

	// Non-empty name indicates to set the name
	if len(sl.names) == 0 {
		return sl.Named(name)
	}
	sl.names = []string{}
	sl.slog = sl.oriSlog
	sl.oriSlog = nil
	return sl.Named(name)
}

func (s *slogWrapper) With(args ...interface{}) hclog.Logger {
	sl := s.clone()
	sl.slog = s.slog.With(args...)
	sl.args = append(sl.args, args...)
	return sl
}

func (s *slogWrapper) ImpliedArgs() []interface{} {
	return s.args
}

func (s *slogWrapper) StandardLogger(opts *hclog.StandardLoggerOptions) *log.Logger {
	if opts == nil {
		opts = &hclog.StandardLoggerOptions{}
	}

	return log.New(s.StandardWriter(opts), "", 0)
}

func (s *slogWrapper) StandardWriter(opts *hclog.StandardLoggerOptions) io.Writer {
	newLog := s.clone()
	return &stdlogAdapter{
		log:                      newLog,
		inferLevels:              opts.InferLevels,
		inferLevelsWithTimestamp: opts.InferLevelsWithTimestamp,
		forceLevel:               opts.ForceLevel,
	}
}

func (s *slogWrapper) getLowestLevel() hclog.Level {
	ctx := context.Background()

	var slogLvls []slog.Level
	for lvlSlog := range levelMapFromSlog {
		slogLvls = append(slogLvls, lvlSlog)
	}
	// Sort the slog levels from Trace up to Error
	sort.Slice(slogLvls, func(i, j int) bool {
		return int(slogLvls[i]) < int(slogLvls[j])
	})

	for _, lvlSlog := range slogLvls {
		lvl := levelMapFromSlog[lvlSlog]
		if s.slog.Enabled(ctx, lvlSlog) {
			return lvl
		}
	}
	return hclog.Off
}

// Regex to ignore characters commonly found in timestamp formats from the
// beginning of inputs.
var logTimestampRegexp = regexp.MustCompile(`^[\d\s:/.+-TZ]*`)

// Provides a io.Writer to shim the data out of *log.Logger
// and back into our Logger. This is basically the only way to
// build upon *log.Logger.
type stdlogAdapter struct {
	log                      hclog.Logger
	inferLevels              bool
	inferLevelsWithTimestamp bool
	forceLevel               hclog.Level
}

// Take the data, infer the levels if configured, and send it through
// a regular Logger.
func (s *stdlogAdapter) Write(data []byte) (int, error) {
	str := string(bytes.TrimRight(data, " \t\n"))

	if s.forceLevel != hclog.NoLevel {
		// Use pickLevel to strip log levels included in the line since we are
		// forcing the level
		_, str := s.pickLevel(str)

		// Log at the forced level
		s.dispatch(str, s.forceLevel)
	} else if s.inferLevels {
		if s.inferLevelsWithTimestamp {
			str = s.trimTimestamp(str)
		}

		level, str := s.pickLevel(str)
		s.dispatch(str, level)
	} else {
		s.log.Info(str)
	}

	return len(data), nil
}

func (s *stdlogAdapter) dispatch(str string, level hclog.Level) {
	switch level {
	case hclog.Trace:
		s.log.Trace(str)
	case hclog.Debug:
		s.log.Debug(str)
	case hclog.Info:
		s.log.Info(str)
	case hclog.Warn:
		s.log.Warn(str)
	case hclog.Error:
		s.log.Error(str)
	default:
		s.log.Info(str)
	}
}

// Detect, based on conventions, what log level this is.
func (s *stdlogAdapter) pickLevel(str string) (hclog.Level, string) {
	switch {
	case strings.HasPrefix(str, "[DEBUG]"):
		return hclog.Debug, strings.TrimSpace(str[7:])
	case strings.HasPrefix(str, "[TRACE]"):
		return hclog.Trace, strings.TrimSpace(str[7:])
	case strings.HasPrefix(str, "[INFO]"):
		return hclog.Info, strings.TrimSpace(str[6:])
	case strings.HasPrefix(str, "[WARN]"):
		return hclog.Warn, strings.TrimSpace(str[6:])
	case strings.HasPrefix(str, "[ERROR]"):
		return hclog.Error, strings.TrimSpace(str[7:])
	case strings.HasPrefix(str, "[ERR]"):
		return hclog.Error, strings.TrimSpace(str[5:])
	default:
		return hclog.Info, str
	}
}

func (s *stdlogAdapter) trimTimestamp(str string) string {
	idx := logTimestampRegexp.FindStringIndex(str)
	return str[idx[1]:]
}
