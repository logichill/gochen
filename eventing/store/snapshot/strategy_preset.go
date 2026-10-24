package snapshot

import (
	"time"
)

// PresetSnapshotStrategies 预设快照策略。
type PresetSnapshotStrategies[ID comparable] struct{}

func (PresetSnapshotStrategies[ID]) DefaultStrategy() ISnapshotStrategy[ID] {
	return NewEventCountStrategy[ID](100)
}

func (PresetSnapshotStrategies[ID]) AggressiveStrategy(snapshotStore ISnapshotStore[ID]) ISnapshotStrategy[ID] {
	return NewCompositeSnapshotStrategy[ID](
		CompositeModeAny,
		NewEventCountStrategy[ID](50),
		NewTimeDurationStrategy[ID](12*time.Hour, snapshotStore),
	)
}

func (PresetSnapshotStrategies[ID]) ConservativeStrategy(snapshotStore ISnapshotStore[ID]) ISnapshotStrategy[ID] {
	return NewCompositeSnapshotStrategy[ID](
		CompositeModeAll,
		NewEventCountStrategy[ID](200),
		NewTimeDurationStrategy[ID](48*time.Hour, snapshotStore),
	)
}

func (PresetSnapshotStrategies[ID]) HighVolumeStrategy(snapshotStore ISnapshotStore[ID]) ISnapshotStrategy[ID] {
	return NewCompositeSnapshotStrategy[ID](
		CompositeModeAny,
		NewEventCountStrategy[ID](20),
		NewAggregateSizeStrategy[ID](500, 512*1024, snapshotStore), // 500事件或512KB
		NewTimeDurationStrategy[ID](6*time.Hour, snapshotStore),
	)
}
