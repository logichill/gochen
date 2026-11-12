package auth

import (
	"fmt"
	"sort"
	"strings"

	"gochen/errors"
)

// Entry 描述独立注册源中的稳定入口；具体传输类型由组合根投影。
type Entry struct {
	Kind string
	Key  string
}

// EntryBinding 连接实际入口与已验证的执行计划。
// Source 记录资源访问断言来源，ConditionPath 记录资源条件的执行入口。
// 公共基础入口通过 AllowReason 和 ResourceAccessNone 显式登记。
type EntryBinding struct {
	Entry          Entry
	Module         string
	Action         string
	Plan           *Plan
	ResourceAccess ResourceAccess
	Source         string
	ConditionPath  string
	AllowReason    string
	Resources      []ResourceBinding
}

// ResourceBinding 声明同一入口必须执行的额外资源计划及条件路径。
type ResourceBinding struct {
	Name          string
	Action        string
	Plan          *Plan
	Source        string
	ConditionPath string
}

// Copy 返回入口与全部资源计划的独立副本。
func (b EntryBinding) Copy() EntryBinding {
	if b.Plan != nil {
		plan := b.Plan.Copy()
		b.Plan = &plan
	}
	b.Resources = append([]ResourceBinding(nil), b.Resources...)
	for i := range b.Resources {
		if b.Resources[i].Plan != nil {
			plan := b.Resources[i].Plan.Copy()
			b.Resources[i].Plan = &plan
		}
	}
	return b
}

// EntryClass 是启动清单的三种分类。
type EntryClass string

const (
	EntryDeclared EntryClass = "declared"
	EntryAllowed  EntryClass = "explicit_allow"
	EntryInvalid  EntryClass = "unbound_or_invalid"
)

// EntryStatus 是独立入口与授权绑定交叉核对后的诊断。
type EntryStatus struct {
	EntryBinding
	Class      EntryClass
	Diagnostic string
}

// ValidateEntries 核对独立注册清单与授权绑定；缺失、冲突及失效绑定阻断启用。
func ValidateEntries(actual []Entry, bindings []EntryBinding) ([]EntryStatus, error) {
	registered := make(map[Entry]int, len(actual))
	for _, entry := range actual {
		registered[entry]++
	}
	byEntry := make(map[Entry][]EntryBinding, len(bindings))
	for _, binding := range bindings {
		binding = binding.Copy()
		byEntry[binding.Entry] = append(byEntry[binding.Entry], binding)
	}
	keys := make(map[Entry]struct{}, len(registered)+len(byEntry))
	for entry := range registered {
		keys[entry] = struct{}{}
	}
	for entry := range byEntry {
		keys[entry] = struct{}{}
	}
	ordered := make([]Entry, 0, len(keys))
	for entry := range keys {
		ordered = append(ordered, entry)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Kind == ordered[j].Kind {
			return ordered[i].Key < ordered[j].Key
		}
		return ordered[i].Kind < ordered[j].Kind
	})
	var report []EntryStatus
	invalid := 0
	for _, entry := range ordered {
		status := EntryStatus{EntryBinding: EntryBinding{Entry: entry}, Class: EntryInvalid}
		matched := byEntry[entry]
		if len(matched) == 1 {
			status.EntryBinding = matched[0]
		}
		switch {
		case strings.TrimSpace(entry.Kind) == "" || strings.TrimSpace(entry.Key) == "":
			status.Diagnostic = "entry requires a kind and stable key"
		case registered[entry] == 0:
			status.Diagnostic = "binding has no actual registered entry"
		case registered[entry] > 1:
			status.Diagnostic = "duplicate actual entry registration"
		case len(matched) == 0:
			status.Diagnostic = "entry has no authorization binding"
		case len(matched) > 1:
			status.Diagnostic = "entry has conflicting authorization bindings"
		default:
			status.Class, status.Diagnostic = validateEntryBinding(status.EntryBinding)
		}
		if status.Class == EntryInvalid {
			invalid++
		}
		report = append(report, status)
	}
	if invalid > 0 {
		return report, errors.NewCode(errors.InvalidInput, "authorization entry validation failed").WithContext("invalid_entries", invalid).WithContext("entries", report)
	}
	return report, nil
}

func validateEntryBinding(b EntryBinding) (EntryClass, string) {
	class, diagnostic := validatePrimaryBinding(b)
	if class == EntryInvalid {
		return class, diagnostic
	}
	if len(b.Resources) > 0 && (b.Plan == nil || b.ResourceAccess != ResourceAccessData) {
		return EntryInvalid, "additional resources require a primary data execution plan"
	}
	seen := map[string]bool{}
	for _, resource := range b.Resources {
		if strings.TrimSpace(resource.Name) == "" || seen[resource.Name] {
			return EntryInvalid, "additional resource requires a unique name"
		}
		seen[resource.Name] = true
		resourceClass, diagnostic := validatePrimaryBinding(EntryBinding{Plan: resource.Plan, Action: resource.Action, ResourceAccess: ResourceAccessData, Source: resource.Source, ConditionPath: resource.ConditionPath})
		if resource.Plan == nil || resourceClass == EntryInvalid {
			return EntryInvalid, fmt.Sprintf("resource %s: %s", resource.Name, diagnostic)
		}
		if resourceClass == EntryDeclared {
			class = EntryDeclared
		}
	}
	return class, ""
}

func validatePrimaryBinding(b EntryBinding) (EntryClass, string) {
	if strings.TrimSpace(b.Source) == "" {
		return EntryInvalid, "resource access assertion requires a source"
	}
	if b.ResourceAccess != ResourceAccessNone && b.ResourceAccess != ResourceAccessData {
		return EntryInvalid, "resource access must be declared"
	}
	if b.Plan == nil {
		if b.ResourceAccess != ResourceAccessNone || b.Action != "" || b.ConditionPath != "" {
			return EntryInvalid, "resource execution requires a compiled plan"
		}
		if strings.TrimSpace(b.AllowReason) == "" {
			return EntryInvalid, "explicit allow requires a reason"
		}
		return EntryAllowed, ""
	}
	if b.AllowReason != "" {
		return EntryInvalid, "plan binding and standalone allow conflict"
	}
	if err := b.Plan.Validate(); err != nil {
		return EntryInvalid, fmt.Sprint(err)
	}
	if _, ok := b.Plan.Descriptor.Action(b.Action); !ok {
		return EntryInvalid, "action is absent from compiled plan"
	}
	if b.Plan.ResourceAccess != b.ResourceAccess {
		return EntryInvalid, "plan resource access conflicts with execution assertion"
	}
	if b.ResourceAccess == ResourceAccessData && strings.TrimSpace(b.ConditionPath) == "" {
		return EntryInvalid, "resource execution requires a condition execution path"
	}
	if b.ResourceAccess == ResourceAccessNone && b.ConditionPath != "" {
		return EntryInvalid, "action-only execution has a resource condition path"
	}
	if strings.TrimSpace(b.Plan.AllowReasons[b.Action]) != "" {
		return EntryAllowed, ""
	}
	return EntryDeclared, ""
}

// BindExecution 在执行契约验证后生成入口绑定；消费端提供自身的条件执行路径。
func BindExecution(authorizer IAuthorizer, descriptor Descriptor, access ResourceAccess, source, conditionPath, action string) (EntryBinding, error) {
	plan, bound := PlanOf(authorizer)
	if !bound {
		return EntryBinding{}, errors.NewCode(errors.InvalidInput, "execution has no compiled authorization plan")
	}
	if err := ValidatePlanBinding(authorizer, descriptor, access); err != nil {
		return EntryBinding{}, err
	}
	binding := EntryBinding{Plan: &plan, ResourceAccess: access, Source: source, ConditionPath: conditionPath, Action: action}
	if class, diagnostic := validateEntryBinding(binding); class == EntryInvalid {
		return EntryBinding{}, errors.NewCode(errors.InvalidInput, diagnostic)
	}
	return binding, nil
}
