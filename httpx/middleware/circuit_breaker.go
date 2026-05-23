package middleware

import (
	"gochen/httpx"
	"gochen/policy/circuit"
)

// CircuitBreakerConfig 熔断器配置。
type CircuitBreakerConfig = circuit.Config

// CircuitBreaker 熔断器中间件。
func CircuitBreaker(cfg CircuitBreakerConfig) httpx.Middleware {
	b := circuit.New(cfg)
	return func(ctx httpx.IContext, next func() error) error {
		return b.Call(next)
	}
}
