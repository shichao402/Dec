package app

import "time"

type EventLevel string

const (
	EventInfo  EventLevel = "info"
	EventWarn  EventLevel = "warn"
	EventError EventLevel = "error"
)

type Progress struct {
	Phase   string
	Current int
	Total   int
}

type OperationEvent struct {
	Time     time.Time
	Level    EventLevel
	Scope    string
	Message  string
	Progress *Progress
}

type Reporter interface {
	Emit(OperationEvent)
}

type ReporterFunc func(OperationEvent)

func (f ReporterFunc) Emit(event OperationEvent) {
	if f != nil {
		f(event)
	}
}

type NopReporter struct{}

func (NopReporter) Emit(OperationEvent) {}

func defaultReporter(reporter Reporter) Reporter {
	if reporter == nil {
		return NopReporter{}
	}
	return reporter
}

func emit(reporter Reporter, level EventLevel, scope, message string, progress *Progress) {
	defaultReporter(reporter).Emit(OperationEvent{
		Time:     time.Now(),
		Level:    level,
		Scope:    scope,
		Message:  message,
		Progress: progress,
	})
}
