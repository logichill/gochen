package eventsourced

import (
	"strings"

	"gochen/app/internal/execution"
	"gochen/auth"
	"gochen/errors"
)

// 标准动作名。自定义动作使用调用方提供的动作名。
const (
	ActionRead      = execution.ActionRead
	ActionCreate    = execution.ActionCreate
	ActionUpdate    = execution.ActionUpdate
	ActionAuditRead = execution.ActionAuditRead
)

type actionPermissions map[string]string

// standardActions 构造事件溯源的标准动作清单；permissions 按动作名覆盖默认权限码。
func standardActions(resource string, permissions actionPermissions, withAudit bool) ([]auth.Action, error) {
	resource = strings.TrimSpace(resource)
	names := []string{ActionRead, ActionCreate, ActionUpdate}
	if withAudit {
		names = append(names, ActionAuditRead)
	}
	known := make(map[string]struct{}, len(names))
	for _, name := range names {
		known[name] = struct{}{}
	}
	for name := range permissions {
		if _, ok := known[name]; !ok {
			return nil, errors.NewCode(errors.InvalidInput, "action permission override references an unknown action").
				WithContext("action", name)
		}
	}

	permissionFor := func(name string) string {
		if code := strings.TrimSpace(permissions[name]); code != "" {
			return code
		}
		if resource == "" {
			return ""
		}
		return resource + ":api:" + name
	}

	actions := make([]auth.Action, 0, len(names))
	for _, name := range names {
		kind := auth.KindRead
		switch name {
		case ActionCreate:
			kind = auth.KindCreate
		case ActionUpdate:
			kind = auth.KindUpdate
		}
		actions = append(actions, auth.Action{
			Name:       name,
			Permission: permissionFor(name),
			Kind:       kind,
		})
	}
	return actions, nil
}
