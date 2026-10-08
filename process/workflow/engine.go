package workflow

import (
	"sync"
	"time"

	"gochen/clock"
)

// Engine 基于显式定义与实例状态推进工作流。
type Engine struct {
	store        IStore
	clkMu        sync.RWMutex
	clk          clock.IClock
	evaluator    IConditionEvaluator
	interceptors []Interceptor
	nodeHooks    nodeHookRegistry

	// definitionLocks 在进程内串行化同一个 definition 的保存，避免并发更新覆盖 CreatedAt 与版本错乱。
	definitionMu    sync.Mutex
	definitionLocks map[string]*keyedLock[string]

	// instanceLocks 在进程内串行化同一个实例的状态推进，避免并发读改写互相覆盖。
	instanceMu    sync.Mutex
	instanceLocks map[ID]*keyedLock[ID]
}

// NewEngine 创建工作流引擎。
func NewEngine(store IStore) *Engine {
	return (&Engine{
		store:           store,
		evaluator:       NewDefaultConditionEvaluator(),
		definitionLocks: map[string]*keyedLock[string]{},
		instanceLocks:   map[ID]*keyedLock[ID]{},
	}).WithClock(clock.NewRealClock())
}

// WithClock 为引擎注入时钟；可在引擎运行期间调用，内部会同步后续 now 读取。
func (e *Engine) WithClock(clk clock.IClock) *Engine {
	if e == nil {
		return e
	}
	if clk != nil {
		e.clkMu.Lock()
		e.clk = clk
		e.clkMu.Unlock()
	}
	return e
}

// WithEvaluator 注入自定义条件表达式求值器。
func (e *Engine) WithEvaluator(evaluator IConditionEvaluator) *Engine {
	if e == nil {
		return e
	}
	if evaluator != nil {
		e.evaluator = evaluator
	}
	return e
}

// Use 注册全局工作流拦截器。
//
// 必须在引擎开始处理实例前完成注册；注册本身不是并发安全的。
func (e *Engine) Use(interceptors ...Interceptor) *Engine {
	if e == nil {
		return e
	}
	e.interceptors = append(e.interceptors, interceptors...)
	return e
}

// now 返回当前时钟时间；未显式注入时钟时使用真实时钟。
func (e *Engine) now() time.Time {
	e.clkMu.RLock()
	clk := e.clk
	e.clkMu.RUnlock()
	if clk == nil {
		return clock.NewRealClock().Now()
	}
	return clk.Now()
}
