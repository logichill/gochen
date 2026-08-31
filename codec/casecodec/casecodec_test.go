package casecodec_test

import (
	"testing"

	"gochen/testkit/require"

	"gochen/codec/casecodec"
	"gochen/errors"
)

// 兼容基线：下表由改造前的 textcase.Snake 实测生成。
//
// snake 渲染结果直接决定 ORM 列名，任何漂移都会让既有表对不上，
// 因此这张表是本次重构的安全网，不得随实现调整而"顺手更新"。
var snakeGolden = map[string]string{
	"":                "",
	"ID":              "id",
	"Id":              "id",
	"iD":              "i_d",
	"A":               "a",
	"a":               "a",
	"UserID":          "user_id",
	"UserId":          "user_id",
	"userID":          "user_id",
	"user_id":         "user_id",
	"user-id":         "user_id",
	"USER_ID":         "user_id",
	"HTTPServer":      "http_server",
	"APIKey":          "api_key",
	"OAuth2Token":     "oauth2_token",
	"IDs":             "ids",
	"ManagedScopeID":  "managed_scope_id",
	"TenantID":        "tenant_id",
	"CreatedAt":       "created_at",
	"DeletedBy":       "deleted_by",
	"Version":         "version",
	"Address1Line":    "address1_line",
	"Line1":           "line1",
	"X1Y2":            "x1_y2",
	"HTTP":            "http",
	"HTTPSProxy":      "https_proxy",
	"AlreadySnake":    "already_snake",
	"already_snake":   "already_snake",
	"Mixed_Case_Name": "mixed_case_name",
	"Kebab-Case-Name": "kebab_case_name",
	"ABCd":            "ab_cd",
	"AbCd":            "ab_cd",
	"aB":              "a_b",
	"Ab":              "ab",
}

func TestSnakeMatchesLegacyBaseline(t *testing.T) {
	for in, want := range snakeGolden {
		got, err := casecodec.Convert(casecodec.Snake, in)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

// Words 是枢纽：一次解析，渲染成任意风格。
func TestRenderAllStylesFromSameWords(t *testing.T) {
	words := casecodec.Parse("ManagedScopeID")
	require.Equal(t, casecodec.Words{"managed", "scope", "id"}, words)

	for _, tc := range []struct {
		codec casecodec.ICodec
		want  string
	}{
		{casecodec.Snake, "managed_scope_id"},
		{casecodec.Kebab, "managed-scope-id"},
		{casecodec.ScreamingSnake, "MANAGED_SCOPE_ID"},
		{casecodec.Camel, "managedScopeId"},
		{casecodec.Pascal, "ManagedScopeId"},
	} {
		got, err := tc.codec.Encode(words)
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}
}

// Decode 风格无关：四种输入解析出同一组词元。
func TestDecodeIsStyleAgnostic(t *testing.T) {
	want := casecodec.Words{"user", "id"}
	for _, in := range []string{"user_id", "user-id", "userID", "UserID", "USER_ID"} {
		got, err := casecodec.Snake.Decode(in)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

// 往返性质一：Decode(Encode(w)) == w，对每种风格恒成立。
func TestRoundTripFromWordsHolds(t *testing.T) {
	for _, words := range []casecodec.Words{
		{"user", "id"},
		{"managed", "scope", "id"},
		{"http", "server"},
		{"line1"},
	} {
		for _, c := range []casecodec.ICodec{
			casecodec.Snake, casecodec.Kebab, casecodec.ScreamingSnake,
			casecodec.Camel, casecodec.Pascal,
		} {
			encoded, err := c.Encode(words)
			require.NoError(t, err)
			decoded, err := c.Decode(encoded)
			require.NoError(t, err)
			require.Equal(t, words, decoded)
		}
	}
}

// 往返性质二：Encode(Decode(s)) == s 仅当 s 已是该风格的规范形式。
func TestRoundTripFromStringOnlyHoldsForCanonicalForm(t *testing.T) {
	canonical, err := casecodec.Convert(casecodec.Snake, "user_id")
	require.NoError(t, err)
	require.Equal(t, "user_id", canonical)

	// 反例：缩写边界在解析成词元时已丢失，渲染侧无从还原。
	// 这是包注释里写明的性质，不是缺陷——用它兜住"以后有人指望往返"的风险。
	got, err := casecodec.Convert(casecodec.Pascal, "UserID")
	require.NoError(t, err)
	require.Equal(t, "UserId", got)
}

func TestParseEdgeCases(t *testing.T) {
	require.Nil(t, casecodec.Parse(""))
	// 连续分隔符不产生空词元。
	require.Equal(t, casecodec.Words{"user", "id"}, casecodec.Parse("user__id"))
	require.Equal(t, casecodec.Words{"user", "id"}, casecodec.Parse("--user--id--"))
}

func TestConvertRejectsNilCodec(t *testing.T) {
	_, err := casecodec.Convert(nil, "UserID")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}
