package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync/atomic"
	"time"
)

// fallbackDecisionIDSeq 内联降级 ID 的单调序列，保证同纳秒并发下的唯一性。
var fallbackDecisionIDSeq atomic.Uint64

// fallbackDecisionID 在决策 ID 生成器故障（非严格模式）时提供确定性兜底：
// 时间戳 + 序列 + 随机熵，格式 "fallback-<unix_nano>-<seq>-<random>"。
// 仅用于保证审计条目主键非空；它不代表决策内容，与严格模式下的拒绝路径无关。
func fallbackDecisionID() string {
	var randBytes [4]byte
	if _, err := rand.Read(randBytes[:]); err != nil {
		randBytes[0] = byte(time.Now().UnixNano() >> 24)
	}
	return "fallback-" +
		time.Now().Format("20060102150405.000000000") + "-" +
		itoaUint64(fallbackDecisionIDSeq.Add(1)) + "-" +
		hex.EncodeToString(randBytes[:])
}

func itoaUint64(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
