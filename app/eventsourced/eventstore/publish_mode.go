package eventstore

import (
	"gochen/app/internal/depcheck"
	"gochen/errors"
)

// PublishMode 指定事件持久化后的发布责任。零值无效，调用方必须显式选择。
type PublishMode string

const (
	// PublishModePersistOnly 仅追加事件，不配置发布依赖。
	PublishModePersistOnly PublishMode = "persist_only"
	// PublishModeDirect 追加成功后通过 EventBus 直接发布；两步不具备原子性。
	PublishModeDirect PublishMode = "direct"
	// PublishModeOutbox 原子保存事件与 Outbox 记录，由后台 Publisher 发布。
	PublishModeOutbox PublishMode = "outbox"
)

func (opts DomainEventStoreOptions[ID]) validatePublishMode() error {
	invalid := func(message string) error {
		return errors.NewCode(errors.InvalidInput, message).WithContext("publish_mode", string(opts.PublishMode))
	}
	switch opts.PublishMode {
	case PublishModePersistOnly:
		if opts.EventBus != nil || opts.OutboxRepo != nil {
			return invalid("persist-only mode forbids event bus and outbox repository")
		}
	case PublishModeDirect:
		if depcheck.IsNil(opts.EventBus) {
			return invalid("direct mode requires event bus")
		}
		if opts.OutboxRepo != nil {
			return invalid("direct mode forbids outbox repository")
		}
	case PublishModeOutbox:
		if depcheck.IsNil(opts.OutboxRepo) {
			return invalid("outbox mode requires outbox repository")
		}
		if opts.EventBus != nil {
			return invalid("outbox mode forbids direct event bus")
		}
	default:
		return invalid("publish mode must be persist_only, direct, or outbox")
	}
	return nil
}
