package gop2pt

import (
	"log"
	"os"
)

type Logger interface {
	Error(msg string, keysAndValues ...interface{})
	Info(msg string, keysAndValues ...interface{})
	Debug(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
}

type defaultLog struct {
	*log.Logger
}

func defaultLogger() *defaultLog {
	return &defaultLog{Logger: log.New(os.Stderr, "p2pt ", log.LstdFlags)}
}

func (l *defaultLog) Error(f string, v ...interface{}) {
	l.Printf("ERROR: "+f, v...)
}

func (l *defaultLog) Warn(f string, v ...interface{}) {
	l.Printf("WARNING: "+f, v...)
}

func (l *defaultLog) Info(f string, v ...interface{}) {
	l.Printf("INFO: "+f, v...)
}

func (l *defaultLog) Debug(f string, v ...interface{}) {
	l.Printf("DEBUG: "+f, v...)
}
