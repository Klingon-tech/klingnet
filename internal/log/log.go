// Package log provides structured, colored logging for Klingnet.
package log

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Logger is the global logger instance.
var Logger zerolog.Logger

// Component loggers for different parts of the system.
var (
	Chain     zerolog.Logger
	P2P       zerolog.Logger
	RPC       zerolog.Logger
	Consensus zerolog.Logger
	Mempool   zerolog.Logger
	Wallet    zerolog.Logger
	Storage   zerolog.Logger
	SubChain  zerolog.Logger
)

func init() {
	// Default to colored console output
	Logger = NewConsoleLogger(os.Stdout, "info")
	initComponentLoggers()
}

// Init initializes the logger with the given configuration.
// When file is non-empty, logs are written to both the console (colored or
// JSON depending on jsonOutput) and the file (always JSON for machine parsing).
func Init(level string, jsonOutput bool, file string) error {
	if file != "" {
		f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}

		lvl := parseLevel(level)

		// Console writer (stdout): colored or JSON per flag.
		var consoleWriter io.Writer
		if jsonOutput {
			consoleWriter = os.Stdout
		} else {
			consoleWriter = zerolog.ConsoleWriter{
				Out:        os.Stdout,
				TimeFormat: "15:04:05",
				NoColor:    false,
			}
		}

		// File writer: always JSON (no ANSI codes, structured for parsing).
		multi := zerolog.MultiLevelWriter(consoleWriter, f)
		Logger = zerolog.New(multi).
			Level(lvl).
			With().
			Timestamp().
			Logger()
	} else if jsonOutput {
		Logger = NewJSONLogger(os.Stdout, level)
	} else {
		Logger = NewConsoleLogger(os.Stdout, level)
	}

	initComponentLoggers()
	return nil
}

// NewConsoleLogger creates a colored console logger.
func NewConsoleLogger(w io.Writer, level string) zerolog.Logger {
	output := zerolog.ConsoleWriter{
		Out:        w,
		TimeFormat: "15:04:05",
		NoColor:    false,
	}

	lvl := parseLevel(level)
	return zerolog.New(output).
		Level(lvl).
		With().
		Timestamp().
		Logger()
}

// NewJSONLogger creates a structured JSON logger.
func NewJSONLogger(w io.Writer, level string) zerolog.Logger {
	lvl := parseLevel(level)
	return zerolog.New(w).
		Level(lvl).
		With().
		Timestamp().
		Logger()
}

// parseLevel converts a string level to zerolog.Level.
func parseLevel(level string) zerolog.Level {
	switch level {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

// initComponentLoggers initializes loggers for each component.
func initComponentLoggers() {
	Chain = Logger.With().Str("component", "chain").Logger()
	P2P = Logger.With().Str("component", "p2p").Logger()
	RPC = Logger.With().Str("component", "rpc").Logger()
	Consensus = Logger.With().Str("component", "consensus").Logger()
	Mempool = Logger.With().Str("component", "mempool").Logger()
	Wallet = Logger.With().Str("component", "wallet").Logger()
	Storage = Logger.With().Str("component", "storage").Logger()
	SubChain = Logger.With().Str("component", "subchain").Logger()
}

// WithComponent returns a logger with a component field.
func WithComponent(name string) zerolog.Logger {
	return Logger.With().Str("component", name).Logger()
}

// WithChainID returns a logger with a chain_id field.
func WithChainID(chainID string) zerolog.Logger {
	return Logger.With().Str("chain_id", chainID).Logger()
}

// Debug logs a debug message.
func Debug() *zerolog.Event {
	return Logger.Debug()
}

// Info logs an info message.
func Info() *zerolog.Event {
	return Logger.Info()
}

// Warn logs a warning message.
func Warn() *zerolog.Event {
	return Logger.Warn()
}

// Error logs an error message.
func Error() *zerolog.Event {
	return Logger.Error()
}

// Fatal logs a fatal message and exits.
func Fatal() *zerolog.Event {
	return Logger.Fatal()
}

// Benchmark helper for timing operations.
func Benchmark(name string) func() {
	start := time.Now()
	return func() {
		Logger.Debug().
			Str("operation", name).
			Dur("duration", time.Since(start)).
			Msg("benchmark")
	}
}
