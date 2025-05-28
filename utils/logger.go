package utils

import (
	"os"

	"github.com/sirupsen/logrus"
)

// NewLogger creates and configures a new Logrus logger instance.
func NewLogger() *logrus.Logger {
	log := logrus.New()

	// Configure logger
	log.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02T15:04:05.000Z07:00", // ISO8601 format
		PrettyPrint:     false,                           // Set to true for more readable, multi-line output
	})

	// Output to stdout
	log.SetOutput(os.Stdout)

	// Set log level (e.g., from env var or config)
	// For now, default to Info. Could be configurable.
	log.SetLevel(logrus.InfoLevel)
	// log.SetLevel(logrus.DebugLevel) // Uncomment for more verbose logging

	return log
}
