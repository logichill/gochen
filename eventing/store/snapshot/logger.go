package snapshot

import (
	"gochen/observe/logging"
)

var defaultSnapshotLogger = logging.ComponentLogger("eventstore.snapshot")

func snapshotLogger() logging.ILogger {
	return defaultSnapshotLogger
}

func newSnapshotLogger(base logging.ILogger) logging.ILogger {
	if base == nil {
		return defaultSnapshotLogger
	}
	return logging.ComponentLogger("eventstore.snapshot", base)
}

func snapshotStoreLogger(logger logging.ILogger) logging.ILogger {
	if logger == nil {
		return defaultSnapshotLogger
	}
	return logger
}
