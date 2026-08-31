package monitoring

import "context"

// HandleRoute 执行内置监控路由并返回可安全对外输出的响应体。
func HandleRoute(ctx context.Context, reg *Registry, route Route) (int, any) {
	if reg == nil {
		reg = DefaultRegistry()
	}
	code, body := route.Handle(ctx, reg)
	return code, sanitizeRouteBody(body)
}

func sanitizeRouteBody(v any) any {
	switch body := v.(type) {
	case HealthReport:
		return sanitizeHealthReport(body)
	case Snapshot:
		body.Health = sanitizeHealthReport(body.Health)
		if body.SnapshotManager != nil {
			body.SnapshotManager.Error = ""
		}
		if body.Outbox != nil {
			body.Outbox.Error = ""
		}
		return body
	default:
		return v
	}
}

func sanitizeHealthReport(report HealthReport) HealthReport {
	if len(report.Checks) == 0 {
		return report
	}
	report.Checks = append([]CheckResult(nil), report.Checks...)
	for i := range report.Checks {
		report.Checks[i].Message = ""
		report.Checks[i].Error = ""
	}
	return report
}
