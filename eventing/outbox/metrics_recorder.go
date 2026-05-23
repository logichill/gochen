package outbox

import "time"

// IPublisherMetricsRecorder 抽象Publisher指标Recorder能力接口。
type IPublisherMetricsRecorder interface {
	RecordOutboxDecode(d time.Duration, err bool)
	RecordOutboxPublish(d time.Duration, err bool)
}
