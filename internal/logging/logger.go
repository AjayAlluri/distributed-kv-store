package logging

import (
	"io"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

type Logger struct {
	*logrus.Logger
}

type Config struct {
	Level  string
	Format string
	File   string
}

func NewLogger(config Config) (*Logger, error) {
	logger := logrus.New()

	// Set log level
	level, err := logrus.ParseLevel(config.Level)
	if err != nil {
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	// Set formatter
	switch config.Format {
	case "json":
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		})
	default:
		logger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		})
	}

	// Set output
	if config.File != "" {
		// Create log directory if it doesn't exist
		dir := filepath.Dir(config.File)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}

		file, err := os.OpenFile(config.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return nil, err
		}

		// Write to both file and stdout
		logger.SetOutput(io.MultiWriter(file, os.Stdout))
	} else {
		logger.SetOutput(os.Stdout)
	}

	return &Logger{logger}, nil
}

// WithField adds a single field to the logger
func (l *Logger) WithField(key string, value interface{}) *logrus.Entry {
	return l.Logger.WithField(key, value)
}

// WithFields adds multiple fields to the logger
func (l *Logger) WithFields(fields logrus.Fields) *logrus.Entry {
	return l.Logger.WithFields(fields)
}

// WithError adds an error field to the logger
func (l *Logger) WithError(err error) *logrus.Entry {
	return l.Logger.WithError(err)
}

// Component creates a logger with a component field
func (l *Logger) WithComponent(component string) *logrus.Entry {
	return l.Logger.WithField("component", component)
}

// Request creates a logger with request-specific fields
func (l *Logger) WithRequest(method, path, userAgent string) *logrus.Entry {
	return l.Logger.WithFields(logrus.Fields{
		"method":     method,
		"path":       path,
		"user_agent": userAgent,
	})
}

// Node creates a logger with node-specific fields
func (l *Logger) WithNode(nodeID string) *logrus.Entry {
	return l.Logger.WithField("node_id", nodeID)
}