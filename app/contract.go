// Package app 定义资源执行契约和应用装配的共享描述。
package app

import (
	"gochen/auth"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"reflect"
)

// ResourceContract 显式声明一个资源实例的维度写能力。
type ResourceContract struct {
	Isolation    meta.Ref
	Mutable      []meta.Dimension
	Assignable   []meta.Dimension
	Transferable []meta.Dimension
}

// ExecutionDescriptor 是构造期绑定并冻结的有效资源契约。
type ExecutionDescriptor struct {
	metadata     *meta.Metadata
	isolation    string
	mutable      map[meta.Dimension]bool
	assignable   map[meta.Dimension]bool
	transferable map[meta.Dimension]bool
}

// IExecutionDescriptorProvider 暴露资源实例已验证的有效契约。
type IExecutionDescriptorProvider interface {
	ExecutionDescriptor() ExecutionDescriptor
}

// BindContract 解析字段身份并物化默认维度保护。
func BindContract(metadata *meta.Metadata, contract ResourceContract) (ExecutionDescriptor, error) {
	if metadata == nil {
		return ExecutionDescriptor{}, errors.NewCode(errors.InvalidInput, "entity metadata is required")
	}
	d := ExecutionDescriptor{metadata: metadata, mutable: map[meta.Dimension]bool{}, assignable: map[meta.Dimension]bool{}, transferable: map[meta.Dimension]bool{}}
	if contract.Isolation != (meta.Ref{}) {
		field, err := metadata.Resolve(contract.Isolation)
		if err != nil {
			return ExecutionDescriptor{}, err
		}
		if field.Identity {
			return ExecutionDescriptor{}, errors.NewCode(errors.InvalidInput, "identity cannot be an isolation field")
		}
		d.isolation = field.Name
	}
	for _, group := range []struct {
		dimensions []meta.Dimension
		target     map[meta.Dimension]bool
	}{
		{contract.Mutable, d.mutable}, {contract.Assignable, d.assignable}, {contract.Transferable, d.transferable},
	} {
		for _, dimension := range group.dimensions {
			field, err := metadata.Resolve(meta.ByDimension(dimension))
			if err != nil {
				return ExecutionDescriptor{}, err
			}
			if field.Name == d.isolation {
				return ExecutionDescriptor{}, errors.NewCode(errors.InvalidInput, "isolation field has structural write protection")
			}
			if group.target[dimension] {
				return ExecutionDescriptor{}, errors.NewCode(errors.InvalidInput, "duplicate dimension capability")
			}
			group.target[dimension] = true
		}
	}
	for dim := range d.mutable {
		if d.assignable[dim] || d.transferable[dim] {
			return ExecutionDescriptor{}, errors.NewCode(errors.InvalidInput, "mutable dimension cannot be an authorization assignment or transfer target")
		}
	}
	return d, nil
}

// Metadata 返回共用的只读实体描述。
func (d ExecutionDescriptor) Metadata() *meta.Metadata { return d.metadata }

// IsolationField 返回隔离字段名称；空值表示该资源未声明隔离。
func (d ExecutionDescriptor) IsolationField() string { return d.isolation }

// IsProtected 判断字段是否受普通更新保护。
func (d ExecutionDescriptor) IsProtected(field meta.Field) bool {
	return field.Identity || field.Name == d.isolation || (field.Dimension != "" && !d.mutable[field.Dimension])
}

// ProtectedFields 返回有效保护集合的独立快照。
func (d ExecutionDescriptor) ProtectedFields() []meta.Field {
	var out []meta.Field
	for _, field := range d.metadata.Fields() {
		if d.IsProtected(field) {
			out = append(out, field)
		}
	}
	return out
}

// CanAssign 报告资源是否显式允许服务端赋值该维度。
func (d ExecutionDescriptor) CanAssign(dim meta.Dimension) bool { return d.assignable[dim] }

// CanTransfer 报告资源是否显式允许独立迁移该维度。
func (d ExecutionDescriptor) CanTransfer(dim meta.Dimension) bool { return d.transferable[dim] }

// ValidateVocabulary 校验全部授权引用与有效执行能力。
func (d ExecutionDescriptor) ValidateVocabulary(v auth.Vocabulary) error {
	if err := v.Validate(d.metadata); err != nil {
		return err
	}
	for _, ref := range v.Refs {
		field, err := d.metadata.Resolve(ref)
		if err != nil {
			return err
		}
		if field.Name == d.isolation || d.mutable[field.Dimension] {
			return errors.NewCode(errors.InvalidInput, "authorization reference conflicts with resource contract").WithContext("reference", ref.String())
		}
	}
	for _, dim := range v.Assignable {
		if !d.CanAssign(dim) && !d.CanTransfer(dim) {
			return errors.NewCode(errors.InvalidInput, "vocabulary assignment lacks resource capability").WithContext("dimension", string(dim))
		}
	}
	return nil
}

// CaptureProtected 保存受保护字段的独立标量前值。
func (d ExecutionDescriptor) CaptureProtected(entity any) (map[string]any, error) {
	values := map[string]any{}
	for _, field := range d.ProtectedFields() {
		value, err := field.Read(entity)
		if err != nil {
			return nil, err
		}
		values[field.Name] = value
	}
	return values, nil
}

// CheckProtected 拒绝普通写入改变受保护字段。
func (d ExecutionDescriptor) CheckProtected(before map[string]any, entity any) error {
	for _, field := range d.ProtectedFields() {
		current, err := field.Read(entity)
		if err != nil {
			return err
		}
		previous, exists := before[field.Name]
		if !exists {
			return errors.NewCode(errors.Internal, "protected field snapshot is incomplete")
		}
		if !reflect.DeepEqual(previous, current) {
			return errors.NewCode(errors.Forbidden, "ordinary write changes a protected field").WithContext("field", field.Name)
		}
	}
	return nil
}

// ValidateWriteSpec 验证纯写谓词和新建标识引用限制，统一覆盖字段别名。
func (d ExecutionDescriptor) ValidateWriteSpec(s spec.Spec, creating bool) error {
	if err := s.Err(); err != nil {
		return errors.Wrap(err, errors.Internal, "invalid write spec")
	}
	if s.HasNamed() {
		return errors.NewCode(errors.Internal, "write spec must not contain named conditions")
	}
	for _, ref := range s.Refs() {
		field, err := d.metadata.Resolve(ref)
		if err != nil {
			return errors.Wrap(err, errors.Internal, "write reference has no binding")
		}
		if creating && field.Identity {
			return errors.NewCode(errors.Internal, "create spec must not reference identity")
		}
	}
	return nil
}
