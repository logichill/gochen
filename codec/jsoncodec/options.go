package jsoncodec

// Option 表示 JSON codec 的可选项。
type Option func(c *config)

type config struct {
	useNumber             bool
	rejectTrailingData    bool
	disallowUnknownFields bool
}

// WithUseNumber 设置是否使用 json.Number（默认启用）。
func WithUseNumber(enable bool) Option {
	return func(c *config) {
		c.useNumber = enable
	}
}

// WithRejectTrailingData 设置是否拒绝尾随数据（默认启用）。
func WithRejectTrailingData(enable bool) Option {
	return func(c *config) {
		c.rejectTrailingData = enable
	}
}

// WithDisallowUnknownFields 设置是否拒绝未知字段（默认关闭）。
func WithDisallowUnknownFields(enable bool) Option {
	return func(c *config) {
		c.disallowUnknownFields = enable
	}
}
