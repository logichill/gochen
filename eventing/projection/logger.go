package projection

import "gochen/observe/logging"

func projectionLogger() logging.ILogger {
	return logging.ComponentLogger("projection.manager")
}
