package httpx

// SecurityLayer 定义安全Layer。
type SecurityLayer string

const (
	// SecurityLayerAPI 表示 API 链路（默认不允许 Session 语义，需显式 opt-out）。
	SecurityLayerAPI SecurityLayer = "api"
	// SecurityLayerWeb 表示 Web 链路（默认允许 Session 语义）。
	SecurityLayerWeb SecurityLayer = "web"
)

type securityLayerKey struct{}

func RequestSecurityLayer(ctx IRequestContext) SecurityLayer {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(securityLayerKey{}); v != nil {
		if s, ok := v.(SecurityLayer); ok {
			return s
		}
	}
	return ""
}

func WithSecurityLayer(ctx IRequestContext, layer SecurityLayer) IRequestContext {
	if ctx == nil {
		return nil
	}
	return ctx.WithValue(securityLayerKey{}, layer)
}
