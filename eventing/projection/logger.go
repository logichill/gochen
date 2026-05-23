package projection

import "gochen/logging"

func projectionLogger() logging.ILogger {
	return logging.ComponentLogger("projection.manager")
}
