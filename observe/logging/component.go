package logging

// WithComponent 为 logger 注入 component 字段，返回新的 logger。
func WithComponent(logger ILogger, component string) ILogger {
	logger = ensureLogger(logger)
	if component == "" {
		return logger
	}
	return logger.WithField("component", component)
}

// ComponentLogger 为 logger 注入组件名字段，便于统一标记日志来源。
func ComponentLogger(component string, base ...ILogger) ILogger {
	var logger ILogger
	if len(base) > 0 {
		logger = base[0]
	}
	return WithComponent(logger, component)
}
