package monitoring

import "time"

var now = time.Now

// Now 返回当前时间，测试里可替换底层 `now` 以获得稳定输出。
func Now() time.Time { return now() }
