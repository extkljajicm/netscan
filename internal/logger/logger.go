package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Setup initializes the global logger with appropriate settings
func Setup(debugMode bool) {
	// Set up console writer with colors for local development
	if os.Getenv("ENVIRONMENT") == "development" {
		log.Logger = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		})
	}

	// Set log level
	if debugMode {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	// Add common fields
	log.Logger = log.With().
		Str("service", "netscan").
		Timestamp().
		Logger()
}

// Get returns a logger with context
func Get() zerolog.Logger {
	return log.Logger
}

// With returns a logger with additional context
func With(key string, value interface{}) zerolog.Logger {
	return log.With().Interface(key, value).Logger()
}
