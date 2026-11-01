package auth_test

import (
	"testing"

	"gochen/auth"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit/require"
)

func TestVocabularyValidateAcceptsDeclaredEntries(t *testing.T) {
	available := auth.Vocabulary{
		Roles:  []spec.Role{spec.RoleScope, spec.RoleOwner},
		Fields: []string{"tenant_id", "status"},
		Named:  []string{"active"},
	}
	v := auth.Vocabulary{
		Roles:  []spec.Role{spec.RoleOwner},
		Fields: []string{"status"},
		Named:  []string{"active"},
	}
	require.NoError(t, v.Validate(available))
}

func TestVocabularyValidateRejectsUnknownEntries(t *testing.T) {
	available := auth.Vocabulary{
		Roles:  []spec.Role{spec.RoleScope},
		Fields: []string{"tenant_id"},
		Named:  []string{"active"},
	}
	cases := []struct {
		name string
		voc  auth.Vocabulary
		code errors.ErrorCode
	}{
		{"unknown role", auth.Vocabulary{Roles: []spec.Role{spec.RoleOwner}}, errors.InvalidInput},
		{"invalid role", auth.Vocabulary{Roles: []spec.Role{"bogus"}}, errors.InvalidInput},
		{"unknown field", auth.Vocabulary{Fields: []string{"missing"}}, errors.InvalidInput},
		{"unknown named", auth.Vocabulary{Named: []string{"missing"}}, errors.InvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.voc.Validate(available)
			require.Error(t, err)
			require.ErrorIs(t, err, tc.code)
		})
	}
}

func TestDecisionValidateAgainstRejectsOutsideReferences(t *testing.T) {
	voc := auth.Vocabulary{
		Roles:  []spec.Role{spec.RoleOwner},
		Fields: []string{"status"},
		Named:  []string{"active"},
	}

	require.NoError(t, auth.Decision{}.ValidateAgainst(voc))

	ok := auth.Decision{
		Allowed: true,
		Spec:    spec.And(spec.Eq("status", "open"), spec.Eq(spec.RoleOwner, "u1"), spec.Named("active")),
		Assign:  []auth.Assignment{{Role: spec.RoleOwner, Value: "u1"}},
	}
	require.NoError(t, ok.ValidateAgainst(voc))

	badField := auth.Decision{Allowed: true, Spec: spec.Eq("secret", "x")}
	err := badField.ValidateAgainst(voc)
	require.Error(t, err)
	require.ErrorIs(t, err, errors.Internal)

	badNamed := auth.Decision{Allowed: true, Spec: spec.Named("other")}
	err = badNamed.ValidateAgainst(voc)
	require.Error(t, err)
	require.ErrorIs(t, err, errors.Internal)

	badAssign := auth.Decision{
		Allowed: true,
		Assign:  []auth.Assignment{{Role: spec.RoleScope, Value: "s1"}},
	}
	err = badAssign.ValidateAgainst(voc)
	require.Error(t, err)
	require.ErrorIs(t, err, errors.Internal)
}
