package logger

import (
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	EnvDevelopment = "dev"
	EnvProduction  = "prod"
)

// All methods log a message with some additional context.
// The variadic key-value pairs are treated as they are in With.
type Logger interface {
	Info(msg string, keysAndValues ...any)
	Debug(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
	Fatal(msg string, keysAndValues ...any)
	Sync() // Sync flushes any buffered log entries.
}

type ZapLogger struct {
	SugaredLogger *zap.SugaredLogger
}

func NewZap(env string) *ZapLogger {
	var loggerConfig zap.Config

	switch env {
	case EnvDevelopment:
		loggerConfig = zap.NewDevelopmentConfig()
	case EnvProduction:
		loggerConfig = zap.NewProductionConfig()
	default:
		loggerConfig = zap.NewDevelopmentConfig()
	}

	// YY-MM-DD HH:MM:SS.sss
	loggerConfig.EncoderConfig.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.Format("2006-01-02 15:04:05.000"))
	}
	loggerConfig.DisableCaller = true

	logger := zap.Must(loggerConfig.Build())

	return &ZapLogger{SugaredLogger: logger.Sugar()}
}

func (l *ZapLogger) Info(msg string, keysAndValues ...any) {
	l.SugaredLogger.Infow(msg, keysAndValues...)
}

func (l *ZapLogger) Debug(msg string, keysAndValues ...any) {
	l.SugaredLogger.Debugw(msg, keysAndValues...)
}

func (l *ZapLogger) Warn(msg string, keysAndValues ...any) {
	l.SugaredLogger.Warnw(msg, keysAndValues...)
}

func (l *ZapLogger) Error(msg string, keysAndValues ...any) {
	l.SugaredLogger.Errorw(msg, keysAndValues...)
}

func (l *ZapLogger) Fatal(msg string, keysAndValues ...any) {
	l.SugaredLogger.Fatalw(msg, keysAndValues...)
}

func (l *ZapLogger) Sync() {
	// Ignoring.
	// See: https://github.com/uber-go/zap/issues/328
	_ = l.SugaredLogger.Sync()
}

var _ Logger = (*ZapLogger)(nil)
