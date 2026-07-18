package repository

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

// ============================================
// user × platform quota 缓存
// ============================================

// userPlatformQuotaCacheKey 构造 Redis key
func userPlatformQuotaCacheKey(userID int64, platform string) string {
	return fmt.Sprintf("billing:user_platform_quota:%d:%s", userID, platform)
}

// parseUserPlatformQuotaHash 将 Redis HGETALL 返回的 map[string]string 反序列化为
// *service.UserPlatformQuotaCacheEntry。空 map（key 不存在）返回 nil。
// GetUserPlatformQuotaCache 和 BatchGetUserPlatformQuotaCache 共用此函数，确保解析逻辑一致。
func parseUserPlatformQuotaHash(m map[string]string) *service.UserPlatformQuotaCacheEntry {
	if len(m) == 0 {
		return nil
	}
	parseFloat := func(s string) float64 {
		if s == "" {
			return 0
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			log.Printf("billing_cache: corrupt quota usage field %q (using 0): %v", s, err)
			return 0
		}
		return f
	}
	parseFloatPtr := func(s string) *float64 {
		if s == "" {
			return nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil
		}
		return &f
	}
	parseTimePtr := func(s string) *time.Time {
		if s == "" {
			return nil
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil
		}
		t := time.Unix(n, 0).UTC()
		return &t
	}
	parseInt64 := func(s string) int64 {
		n, _ := strconv.ParseInt(s, 10, 64)
		return n
	}
	return &service.UserPlatformQuotaCacheEntry{
		DailyUsageUSD:      parseFloat(m["daily_usage"]),
		WeeklyUsageUSD:     parseFloat(m["weekly_usage"]),
		MonthlyUsageUSD:    parseFloat(m["monthly_usage"]),
		Version:            parseInt64(m["version"]),
		SchemaVersion:      parseInt64(m["schema_version"]),
		DailyLimitUSD:      parseFloatPtr(m["daily_limit"]),
		WeeklyLimitUSD:     parseFloatPtr(m["weekly_limit"]),
		MonthlyLimitUSD:    parseFloatPtr(m["monthly_limit"]),
		DailyWindowStart:   parseTimePtr(m["daily_window_start"]),
		WeeklyWindowStart:  parseTimePtr(m["weekly_window_start"]),
		MonthlyWindowStart: parseTimePtr(m["monthly_window_start"]),
	}
}

func (c *billingCache) GetUserPlatformQuotaCache(ctx context.Context, userID int64, platform string) (*service.UserPlatformQuotaCacheEntry, bool, error) {
	key := userPlatformQuotaCacheKey(userID, platform)
	m, err := c.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, false, err
	}
	entry := parseUserPlatformQuotaHash(m)
	if entry == nil {
		// 空 map → key 不存在 → MISS
		return nil, false, nil
	}
	return entry, true, nil
}

func (c *billingCache) SetUserPlatformQuotaCache(ctx context.Context, userID int64, platform string, entry *service.UserPlatformQuotaCacheEntry, ttl time.Duration) error {
	if entry == nil {
		return nil
	}
	key := userPlatformQuotaCacheKey(userID, platform)
	pipe := c.rdb.TxPipeline()

	// 浮点可空字段：nil → 空字符串（读取时 parseFloatPtr 返回 nil，表示无限额）
	fmtFloatPtr := func(p *float64) string {
		if p == nil {
			return ""
		}
		return strconv.FormatFloat(*p, 'f', -1, 64)
	}
	// time.Time 可空字段：nil → 空字符串；有值 → unix 秒
	fmtTimePtr := func(p *time.Time) string {
		if p == nil {
			return ""
		}
		return strconv.FormatInt(p.Unix(), 10)
	}

	pipe.HSet(ctx, key,
		"daily_usage", entry.DailyUsageUSD,
		"weekly_usage", entry.WeeklyUsageUSD,
		"monthly_usage", entry.MonthlyUsageUSD,
		"version", entry.Version,
		"schema_version", entry.SchemaVersion,
		"daily_limit", fmtFloatPtr(entry.DailyLimitUSD),
		"weekly_limit", fmtFloatPtr(entry.WeeklyLimitUSD),
		"monthly_limit", fmtFloatPtr(entry.MonthlyLimitUSD),
		"daily_window_start", fmtTimePtr(entry.DailyWindowStart),
		"weekly_window_start", fmtTimePtr(entry.WeeklyWindowStart),
		"monthly_window_start", fmtTimePtr(entry.MonthlyWindowStart),
	)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *billingCache) DeleteUserPlatformQuotaCache(ctx context.Context, userID int64, platform string) error {
	return c.rdb.Del(ctx, userPlatformQuotaCacheKey(userID, platform)).Err()
}

// updateUserPlatformQuotaUsageScript 缓存累加：EXISTS + schema_version 双重守卫。
// 旧版 entry（schema_version != ARGV[3]，包括缺字段的 0 值）不参与累加，由上层走 DB fallback 后
// SetCache 重建为新版 entry —— 若此处仍累加，上层覆盖时会丢失这部分增量，导致 Redis usage 比真实偏小。
// key 不存在同样跳过（由下次 SetCache 重建）。
// KEYS[1] = hash key
// KEYS[2] = 脏集 key（dirty set）
// ARGV[1] = cost (string float)
// ARGV[2] = ttl seconds
// ARGV[3] = expected schema_version (Go 侧 UserPlatformQuotaCacheSchemaV1)
// ARGV[4] = dirty set member（空串则不 SADD）
// ARGV[5] = 脏集兜底 TTL 秒
const updateUserPlatformQuotaUsageScript = `
if redis.call("EXISTS", KEYS[1]) == 0 then
    return 0
end
local ver = redis.call("HGET", KEYS[1], "schema_version")
if ver == false or tonumber(ver) ~= tonumber(ARGV[3]) then
    return 0
end
redis.call("HINCRBYFLOAT", KEYS[1], "daily_usage", ARGV[1])
redis.call("HINCRBYFLOAT", KEYS[1], "weekly_usage", ARGV[1])
redis.call("HINCRBYFLOAT", KEYS[1], "monthly_usage", ARGV[1])
redis.call("HINCRBY", KEYS[1], "version", 1)
redis.call("EXPIRE", KEYS[1], ARGV[2])
if ARGV[4] ~= "" then
    redis.call("SADD", KEYS[2], ARGV[4])
    redis.call("EXPIRE", KEYS[2], ARGV[5])
end
return 1
`

// userPlatformQuotaDirtySetKey 返回脏集（dirty set）的 Redis key。
// 使用与 userPlatformQuotaCacheKey 相同的前缀 "billing:"。
func userPlatformQuotaDirtySetKey() string { return "billing:" + "upq:dirty" }

// userPlatformQuotaDirtyTTLSeconds 脏集兜底 TTL（秒）：初始 SADD（Lua）与 Readd 共用，
// 确保 flusher 长期停摆时脏集最终过期；正常运行因持续 SADD 不断续期。
const userPlatformQuotaDirtyTTLSeconds = 86400

// userPlatformQuotaDirtyMember 构造脏集成员字符串 "userID:platform"。
func userPlatformQuotaDirtyMember(userID int64, platform string) string {
	return strconv.FormatInt(userID, 10) + ":" + platform
}

func (c *billingCache) IncrUserPlatformQuotaUsageCache(ctx context.Context, userID int64, platform string, cost float64, ttl time.Duration, markDirty bool) error {
	member := ""
	if markDirty {
		member = userPlatformQuotaDirtyMember(userID, platform)
	}
	_, err := c.rdb.Eval(ctx, updateUserPlatformQuotaUsageScript,
		[]string{userPlatformQuotaCacheKey(userID, platform), userPlatformQuotaDirtySetKey()},
		strconv.FormatFloat(cost, 'f', -1, 64),
		int(ttl.Seconds()),
		service.UserPlatformQuotaCacheSchemaV1,
		member,
		userPlatformQuotaDirtyTTLSeconds,
	).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	return nil
}

// parseUserPlatformQuotaDirtyMember 将脏集成员字符串 "userID:platform" 解析为
// service.UserPlatformQuotaKey。解析失败返回 ok=false。
func parseUserPlatformQuotaDirtyMember(m string) (service.UserPlatformQuotaKey, bool) {
	parts := strings.SplitN(m, ":", 2)
	if len(parts) != 2 {
		return service.UserPlatformQuotaKey{}, false
	}
	uid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return service.UserPlatformQuotaKey{}, false
	}
	return service.UserPlatformQuotaKey{UserID: uid, Platform: parts[1]}, true
}

// PopDirtyUserPlatformQuotaKeys 从脏集随机弹出最多 n 个 key。
// 脏集为空时返回 (nil, nil)。
func (c *billingCache) PopDirtyUserPlatformQuotaKeys(ctx context.Context, n int) ([]service.UserPlatformQuotaKey, error) {
	members, err := c.rdb.SPopN(ctx, userPlatformQuotaDirtySetKey(), int64(n)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, err
	}
	keys := make([]service.UserPlatformQuotaKey, 0, len(members))
	for _, m := range members {
		k, ok := parseUserPlatformQuotaDirtyMember(m)
		if !ok {
			log.Printf("billing_cache: skipping invalid dirty member %q", m)
			continue
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// ReaddDirtyUserPlatformQuotaKeys 将 keys 重新加入脏集（flush 失败时回填）。
// 通过 pipeline 同时执行 SAdd + Expire，确保 Readd 后脏集具有兜底 TTL。
// 空切片时直接返回 nil。
func (c *billingCache) ReaddDirtyUserPlatformQuotaKeys(ctx context.Context, keys []service.UserPlatformQuotaKey) error {
	if len(keys) == 0 {
		return nil
	}
	dirtyKey := userPlatformQuotaDirtySetKey()
	members := make([]any, len(keys))
	for i, k := range keys {
		members[i] = userPlatformQuotaDirtyMember(k.UserID, k.Platform)
	}
	pipe := c.rdb.Pipeline()
	pipe.SAdd(ctx, dirtyKey, members...)
	pipe.Expire(ctx, dirtyKey, userPlatformQuotaDirtyTTLSeconds*time.Second)
	_, err := pipe.Exec(ctx)
	return err
}

// BatchGetUserPlatformQuotaCache 通过 Pipeline 批量 HGETALL 获取多个 user×platform 的
// quota cache。返回切片与 keys 顺序、长度对齐；MISS 或解析失败位置返回 nil。
func (c *billingCache) BatchGetUserPlatformQuotaCache(ctx context.Context, keys []service.UserPlatformQuotaKey) ([]*service.UserPlatformQuotaCacheEntry, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	pipe := c.rdb.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(keys))
	for i, k := range keys {
		cmds[i] = pipe.HGetAll(ctx, userPlatformQuotaCacheKey(k.UserID, k.Platform))
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	results := make([]*service.UserPlatformQuotaCacheEntry, len(keys))
	for i, cmd := range cmds {
		m, err := cmd.Result()
		if err != nil {
			if !errors.Is(err, redis.Nil) {
				log.Printf("billing_cache: BatchGet HGETALL cmd[%d] failed: %v (skip, self-heal)", i, err)
			}
			// 单个命令失败 → 对应位置 nil，继续
			continue
		}
		results[i] = parseUserPlatformQuotaHash(m)
	}
	return results, nil
}
