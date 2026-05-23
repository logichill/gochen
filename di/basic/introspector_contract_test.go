package basic

import (
	"testing"

	"gochen/di"
	"gochen/di/contracttest"
)

func TestContainer_IntrospectorContract(t *testing.T) {
	contracttest.RunIntrospectorContractTests(t, func() interface {
		di.IRegistry
		di.IIntrospector
	} {
		return New()
	})
}
