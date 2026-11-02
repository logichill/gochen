package spec

import (
	"gochen/domain/meta"
	"testing"
)

func TestNullableCollectionsMatchNullThroughValueSource(t *testing.T) {
	type entity struct {
		Slice     []string
		Map       map[string]string
		Func      func()
		Interface any
		Pointer   *string
		Scalar    string
	}
	metadata, err := meta.Describe[*entity]()
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		name  string
		value *entity
		want  bool
	}{
		{"nil", &entity{Interface: []string(nil)}, true},
		{"present", &entity{Slice: []string{}, Map: map[string]string{}, Func: func() {}, Interface: []string{}, Pointer: new(string)}, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			for _, name := range []string{"Slice", "Map", "Func", "Interface", "Pointer", "Scalar"} {
				want := fixture.want && name != "Scalar"
				ref := meta.ByField(name)
				_, value, readErr := metadata.Source(fixture.value).Read(ref)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if (value == nil) != want {
					t.Errorf("%s value=%#v, want null=%t", name, value, want)
				}
				matched, err := NewInterpreter().MatchSource(IsNull(ref), metadata.Source(fixture.value))
				if err != nil || matched != want {
					t.Errorf("%s: matched=%t error=%v, want=%t", name, matched, err, want)
				}
			}
		})
	}
}
