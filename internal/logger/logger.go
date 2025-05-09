package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	EnvDev  = "dev"
	EnvProd = "prod"
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
	case EnvDev:
		loggerConfig = zap.NewDevelopmentConfig()
	case EnvProd:
		loggerConfig = zap.NewProductionConfig()
	default:
		loggerConfig = zap.NewDevelopmentConfig()
	}

	loggerConfig.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
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
	err := l.SugaredLogger.Sync()
	if err != nil {
		l.Warn("Logger sync error",
			"error", err,
		)
		l.Fatal(err.Error())
	}
}

var _ Logger = (*ZapLogger)(nil)
