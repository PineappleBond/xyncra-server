package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// --------------------------------------------------------------------------
// Redis key layout
// --------------------------------------------------------------------------
//
// Each connection is stored as two Redis keys:
//
//   xyncra:conn:info:{connID}      -> JSON-encoded ConnectionInfo (string)
//   xyncra:conn:user:{userID}      -> Set of connection IDs for the user
//
// The info key carries a TTL so that connections are automatically evicted
// when they go stale (e.g. server crash without a clean disconnect). The
// user set is maintained via Add / Remove and cleaned up lazily.

const (
	// redisKeyConnInfoPrefix is the key prefix for per-connection info.
	redisKeyConnInfoPrefix = "xyncra:conn:info:"

	// redisKeyConnUserPrefix is the key prefix for the per-user connection set.
	redisKeyConnUserPrefix = "xyncra:conn:user:"

	// defaultConnectionTTL is the default time-to-live for a connection when
	// the ConnectionInfo does not specify a TTL.
	defaultConnectionTTL = 30 * time.Minute

	// maxListByUserLimit is the maximum number of connections returned by
	// ListByUser in a single call.
	maxListByUserLimit = 1000

	// removeByUserBatchSize is the number of keys deleted per UNLINK call
	// in RemoveByUser to avoid blocking Redis with large deletions.
	removeByUserBatchSize = 100
)

// --------------------------------------------------------------------------
// Lua scripts for atomic operations
// --------------------------------------------------------------------------

// luaAdd atomically sets the connection info key and adds the connection ID
// to the user's set. It also handles per-user connection limits and cleans up
// orphaned set entries when a connection's UserID changes on overwrite.
//
// The existence check and old-UserID lookup are performed inside the Lua
// script (not in Go) to eliminate the TOCTOU race where the info key could
// expire between a Go-side GET and the Lua call, allowing a stale "exists"
// flag to bypass the MaxConnectionsPerUser check (R3-001).
//
// KEYS[1] = infoKey, KEYS[2] = newUserKey
// ARGV[1] = data, ARGV[2] = ttl (ms), ARGV[3] = connID,
//   ARGV[4] = maxConns (0 = unlimited), ARGV[5] = newUserID,
//   ARGV[6] = userKeyPrefix (e.g. "xyncra:conn:user:")
//
// Returns 1 on success, -1 if max connections exceeded.
var luaAdd = redis.NewScript(`
	local infoKey = KEYS[1]
	local newUserKey = KEYS[2]
	local data = ARGV[1]
	local ttl = tonumber(ARGV[2])
	local connID = ARGV[3]
	local maxConns = tonumber(ARGV[4])
	local newUserID = ARGV[5]
	local userKeyPrefix = ARGV[6]

	-- Atomically read old data (if any) to determine overwrite vs new.
	local oldUserID = ""
	local getResult = redis.pcall('GET', infoKey)
	if type(getResult) == 'string' then
		local ok, decoded = pcall(cjson.decode, getResult)
		if ok and decoded and decoded.user_id then
			oldUserID = decoded.user_id
		end
	end

	if oldUserID == "" then
		-- New connection: enforce per-user limit.
		if maxConns > 0 and redis.call('SCARD', newUserKey) >= maxConns then
			return -1
		end
	else
		-- Overwrite: if UserID changed, remove from old user's set.
		if oldUserID ~= newUserID then
			redis.call('SREM', userKeyPrefix .. oldUserID, connID)
		end
	end

	redis.call('SET', infoKey, data, 'PX', ttl)
	redis.call('SADD', newUserKey, connID)
	return 1
`)

// luaRemove atomically deletes the connection info key and removes the
// connection ID from the user's set. This fixes the TOCTOU race (A-003)
// and uses a Lua script for true atomicity (A-007).
var luaRemove = redis.NewScript(`
	local infoKey = KEYS[1]
	local userKey = KEYS[2]
	local connID = ARGV[1]

	local existed = redis.call('DEL', infoKey)
	redis.call('SREM', userKey, connID)
	return existed
`)

// luaUpdate atomically updates the connection info using a Lua script.
// It reads the current data, applies the provided update, and writes back
// only if the key exists. This prevents the read-modify-write race (A-004).
var luaUpdate = redis.NewScript(`
	local infoKey = KEYS[1]
	local newData = ARGV[1]
	local ttl = tonumber(ARGV[2])

	if redis.call('EXISTS', infoKey) == 0 then
		return 0
	end
	redis.call('SET', infoKey, newData, 'PX', ttl)
	return 1
`)

// luaRefresh atomically checks whether the connection info key exists and, if
// so, resets its TTL in a single round-trip. This replaces the previous
// Exists + Get + Expire sequence (P2-003/A2-005).
//
// KEYS[1] = infoKey, ARGV[1] = ttl (ms)
// Returns 1 if refreshed, 0 if the key does not exist.
var luaRefresh = redis.NewScript(`
	local infoKey = KEYS[1]
	local ttl = tonumber(ARGV[1])

	if redis.call('EXISTS', infoKey) == 0 then
		return 0
	end
	redis.call('PEXPIRE', infoKey, ttl)
	return 1
`)

// --------------------------------------------------------------------------
// Configuration
// --------------------------------------------------------------------------

// RedisConnectionStoreConfig holds the configuration for a
// RedisConnectionStore.
type RedisConnectionStoreConfig struct {
	// Addr is the Redis server address in "host:port" format.
	Addr string

	// Password is the Redis AUTH password. Leave empty for no authentication.
	Password string

	// DB is the Redis database index.
	DB int

	// KeyPrefix overrides the default key prefix ("xyncra:conn:"). Useful
	// for multi-tenant deployments sharing a single Redis instance.
	KeyPrefix string

	// DefaultTTL overrides the default connection TTL (30 minutes).
	DefaultTTL time.Duration

	// LazyConnect skips the initial connectivity check when true.
	// The store will attempt to connect lazily on the first operation.
	// Use this when Redis may not be immediately available at startup.
	LazyConnect bool

	// MaxConnectionsPerUser limits the number of active connections per
	// user. A value of 0 means no limit.
	MaxConnectionsPerUser int

	// PoolSize sets the maximum number of connections in the Redis pool.
	// A value of 0 uses the go-redis default (10 connections per CPU).
	PoolSize int

	// MinIdleConns sets the minimum number of idle connections in the pool.
	// A value of 0 uses the go-redis default.
	MinIdleConns int

	// PoolTimeout is the maximum time to wait for a pool connection.
	// A value of 0 uses the go-redis default (5 seconds).
	PoolTimeout time.Duration
}

// resolveDefaultTTL returns the configured default TTL or the package-level
// default.
func (c RedisConnectionStoreConfig) resolveDefaultTTL() time.Duration {
	if c.DefaultTTL > 0 {
		return c.DefaultTTL
	}
	return defaultConnectionTTL
}

// toRedisOptions converts the config to redis.Options.
func (c RedisConnectionStoreConfig) toRedisOptions() *redis.Options {
	opts := &redis.Options{
		Addr:     c.Addr,
		Password: c.Password,
		DB:       c.DB,
	}
	if c.PoolSize > 0 {
		opts.PoolSize = c.PoolSize
	}
	if c.MinIdleConns > 0 {
		opts.MinIdleConns = c.MinIdleConns
	}
	if c.PoolTimeout > 0 {
		opts.PoolTimeout = c.PoolTimeout
	}
	return opts
}

// --------------------------------------------------------------------------
// RedisConnectionStore
// --------------------------------------------------------------------------

// RedisConnectionStore implements ConnectionStore using Redis as the backend.
// Connections are stored as JSON-encoded strings with per-key TTLs for
// automatic expiration.
//
// The zero value is not usable; use NewRedisConnectionStore to create an
// instance.
type RedisConnectionStore struct {
	client                *redis.Client
	defaultTTL            time.Duration
	keyPrefix             string
	ownsClient            bool // true when we created the client and should close it
	maxConnsPerUser       int  // 0 means no limit
}

// Ensure RedisConnectionStore implements ConnectionStore at compile time.
var _ ConnectionStore = (*RedisConnectionStore)(nil)

// NewRedisConnectionStore creates a RedisConnectionStore that connects to the
// Redis instance described by cfg. The caller must call Close when the store
// is no longer needed.
//
// If cfg.LazyConnect is true, the initial connectivity check is skipped and
// the store will attempt to connect lazily on the first operation.
func NewRedisConnectionStore(cfg RedisConnectionStoreConfig) (*RedisConnectionStore, error) {
	if cfg.Addr == "" {
		return nil, fmt.Errorf("server: redis address is required")
	}

	client := redis.NewClient(cfg.toRedisOptions())

	// Verify connectivity unless lazy connect is enabled.
	if !cfg.LazyConnect {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Ping(ctx).Err(); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("server: redis ping failed: %w", err)
		}
	}

	return &RedisConnectionStore{
		client:          client,
		defaultTTL:      cfg.resolveDefaultTTL(),
		keyPrefix:       cfg.KeyPrefix,
		ownsClient:      true,
		maxConnsPerUser: cfg.MaxConnectionsPerUser,
	}, nil
}

// RedisConnectionStoreFromClientConfig holds the configuration for creating a
// RedisConnectionStore from an existing redis.Client.
type RedisConnectionStoreFromClientConfig struct {
	// Client is the existing Redis client. The caller retains ownership.
	Client *redis.Client

	// DefaultTTL overrides the default connection TTL (30 minutes).
	DefaultTTL time.Duration

	// KeyPrefix overrides the default key prefix. Useful for multi-tenant
	// deployments.
	KeyPrefix string

	// MaxConnectionsPerUser limits the number of active connections per
	// user. A value of 0 means no limit.
	MaxConnectionsPerUser int
}

// NewRedisConnectionStoreFromClient creates a RedisConnectionStore from an
// existing redis.Client via the provided config. The caller retains ownership
// of the client; Close will not close it.
//
// This replaces the old signature-based constructor to support KeyPrefix and
// other configuration options (A-008).
func NewRedisConnectionStoreFromClient(cfg RedisConnectionStoreFromClientConfig) *RedisConnectionStore {
	if cfg.DefaultTTL <= 0 {
		cfg.DefaultTTL = defaultConnectionTTL
	}
	return &RedisConnectionStore{
		client:          cfg.Client,
		defaultTTL:      cfg.DefaultTTL,
		keyPrefix:       cfg.KeyPrefix,
		maxConnsPerUser: cfg.MaxConnectionsPerUser,
	}
}

// --------------------------------------------------------------------------
// ConnectionStore implementation
// --------------------------------------------------------------------------

// Add registers a new connection or updates an existing one. The connection
// info is JSON-encoded and stored with a TTL. The connection ID is also added
// to the per-user set. Both operations are executed atomically via a Lua
// script (P-003, A-007).
//
// The existence check, per-user connection limit (MaxConnectionsPerUser), and
// old-user set cleanup on UserID change are all performed inside the Lua
// script to avoid TOCTOU races. Previously, the existence check was done in
// Go before the Lua call, which allowed a concurrent TTL expiry to bypass the
// MaxConnectionsPerUser check (R3-001).
//
// If MaxConnectionsPerUser is configured and the user already has that many
// active connections, Add returns ErrMaxConnectionsExceeded.
//
// The UserID field on a ConnectionInfo should be considered immutable after
// the initial Add. Overwriting a connection with a different UserID is
// supported but the old user's set entry is removed; callers should avoid
// relying on cross-user connection migration.
func (s *RedisConnectionStore) Add(ctx context.Context, info *ConnectionInfo) error {
	if info == nil {
		return fmt.Errorf("server: connection info is nil")
	}
	if info.ID == "" {
		return fmt.Errorf("server: connection ID is required")
	}
	if info.UserID == "" {
		return fmt.Errorf("server: user ID is required")
	}

	now := time.Now()
	if info.CreatedAt.IsZero() {
		info.CreatedAt = now
	}
	info.UpdatedAt = now

	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("server: add connection [connID=%s]: marshal: %w", info.ID, err)
	}

	ttl := s.resolveTTL(info.TTL)

	// The existence check and old-UserID lookup are performed inside the Lua
	// script to avoid a TOCTOU race: if the info key expired between a Go-side
	// GET and the Lua call, a stale "exists" flag could bypass the
	// MaxConnectionsPerUser check (R3-001).
	infoKey := s.infoKey(info.ID)
	newUserKey := s.userKey(info.UserID)

	result, err := luaAdd.Run(ctx, s.client,
		[]string{infoKey, newUserKey},
		string(data), fmt.Sprintf("%d", ttl.Milliseconds()), info.ID,
		fmt.Sprintf("%d", s.maxConnsPerUser), info.UserID,
		s.keyPrefix+redisKeyConnUserPrefix).Int()
	if err != nil {
		return fmt.Errorf("server: add connection [connID=%s]: %w", info.ID, classifyRedisError(err))
	}
	if result == -1 {
		return fmt.Errorf("server: add connection [connID=%s]: %w",
			info.ID, ErrMaxConnectionsExceeded)
	}

	return nil
}

// Get retrieves the connection info for the given connection ID. Returns
// ErrConnectionNotFound if the key does not exist.
func (s *RedisConnectionStore) Get(ctx context.Context, connID string) (*ConnectionInfo, error) {
	if connID == "" {
		return nil, fmt.Errorf("server: connection ID is required")
	}

	val, err := s.client.Get(ctx, s.infoKey(connID)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrConnectionNotFound
		}
		return nil, fmt.Errorf("server: get connection [connID=%s]: %w", connID, classifyRedisError(err))
	}

	var info ConnectionInfo
	if err := json.Unmarshal([]byte(val), &info); err != nil {
		return nil, fmt.Errorf("server: get connection [connID=%s]: unmarshal: %w", connID, err)
	}

	return &info, nil
}

// Remove deletes the connection identified by connID. It removes both the
// info key and the connection ID from the user's set atomically via a Lua
// script (P-003, A-003, A-007). It is a no-op if the connection does not
// exist.
//
// Note: Remove first reads the connection info to determine the owning user
// for set cleanup. If the info key expires between the read and the Lua
// deletion, the SREM on the user set still executes (removing a potentially
// stale entry). This is safe because the set entry would be orphaned anyway.
func (s *RedisConnectionStore) Remove(ctx context.Context, connID string) error {
	if connID == "" {
		return fmt.Errorf("server: connection ID is required")
	}

	// Look up the user ID so we can clean up the user set.
	info, err := s.Get(ctx, connID)
	if err != nil {
		if err == ErrConnectionNotFound {
			return nil // no-op
		}
		return err
	}

	if err := luaRemove.Run(ctx, s.client, []string{s.infoKey(connID), s.userKey(info.UserID)},
		connID).Err(); err != nil {
		return fmt.Errorf("server: remove connection [connID=%s, userID=%s]: %w",
			connID, info.UserID, classifyRedisError(err))
	}

	return nil
}

// Exists reports whether a connection with the given ID is currently active.
func (s *RedisConnectionStore) Exists(ctx context.Context, connID string) (bool, error) {
	if connID == "" {
		return false, fmt.Errorf("server: connection ID is required")
	}

	n, err := s.client.Exists(ctx, s.infoKey(connID)).Result()
	if err != nil {
		return false, fmt.Errorf("server: exists connection [connID=%s]: %w", connID, classifyRedisError(err))
	}
	return n > 0, nil
}

// Update modifies the metadata of an existing connection. The connection's
// UpdatedAt is set to the current time and its TTL is refreshed.
//
// Note: Update performs a non-atomic read-modify-write: it reads the current
// ConnectionInfo, replaces its Metadata field, and writes back via a Lua
// script that only checks existence (not CAS). Concurrent Update calls may
// silently overwrite each other's metadata changes. For atomic
// read-modify-write semantics, use Patch instead.
func (s *RedisConnectionStore) Update(ctx context.Context, connID string, metadata map[string]string) error {
	if connID == "" {
		return fmt.Errorf("server: connection ID is required")
	}

	info, err := s.Get(ctx, connID)
	if err != nil {
		return err
	}

	info.Metadata = metadata
	info.UpdatedAt = time.Now()

	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("server: update connection [connID=%s]: marshal: %w", connID, err)
	}

	ttl := s.resolveTTL(info.TTL)
	result, err := luaUpdate.Run(ctx, s.client, []string{s.infoKey(connID)},
		string(data), fmt.Sprintf("%d", ttl.Milliseconds())).Int()
	if err != nil {
		return fmt.Errorf("server: update connection [connID=%s]: %w", connID, classifyRedisError(err))
	}
	if result == 0 {
		return ErrConnectionNotFound
	}

	return nil
}

// Patch applies an updater function to an existing connection. The updater
// receives a copy of the current ConnectionInfo and may modify any field.
//
// Note: the read-modify-write cycle is NOT fully atomic. The current
// ConnectionInfo is read from Redis, the updater is invoked in the client
// process, and the modified copy is written back via a Lua script that only
// checks for key existence (not compare-and-swap). Concurrent Patch calls on
// the same connection may silently overwrite each other's changes. Callers
// that require strict serialisability should implement their own locking.
//
// The UserID and ID fields on ConnectionInfo should not be modified by the
// updater. Changing UserID will update the info key but will NOT update the
// per-user connection set index, leading to inconsistencies. If you need to
// reassign a connection to a different user, use Add with the same
// connection ID instead.
//
// Returns ErrConnectionNotFound if the connection does not exist.
func (s *RedisConnectionStore) Patch(ctx context.Context, connID string, updater func(*ConnectionInfo)) error {
	if connID == "" {
		return fmt.Errorf("server: connection ID is required")
	}
	if updater == nil {
		return fmt.Errorf("server: updater function is nil")
	}

	infoKey := s.infoKey(connID)

	// Read the current connection info.
	val, err := s.client.Get(ctx, infoKey).Result()
	if err != nil {
		if err == redis.Nil {
			return ErrConnectionNotFound
		}
		return fmt.Errorf("server: patch connection [connID=%s]: read: %w", connID, classifyRedisError(err))
	}

	var info ConnectionInfo
	if err := json.Unmarshal([]byte(val), &info); err != nil {
		return fmt.Errorf("server: patch connection [connID=%s]: unmarshal: %w", connID, err)
	}

	// Apply the updater function.
	updater(&info)
	info.UpdatedAt = time.Now()

	newData, err := json.Marshal(&info)
	if err != nil {
		return fmt.Errorf("server: patch connection [connID=%s]: marshal: %w", connID, err)
	}

	ttl := s.resolveTTL(info.TTL)

	// Use Lua script for atomic write (only if key still exists).
	result, err := luaUpdate.Run(ctx, s.client, []string{infoKey},
		string(newData), fmt.Sprintf("%d", ttl.Milliseconds())).Int()
	if err != nil {
		return fmt.Errorf("server: patch connection [connID=%s]: %w", connID, classifyRedisError(err))
	}
	if result == 0 {
		return ErrConnectionNotFound
	}

	return nil
}

// Refresh resets the TTL of the connection, extending its lifetime. The
// existence check and TTL reset are performed atomically in a single Lua
// script to avoid the previous 3-call round-trip (P2-003/A2-005).
// Returns ErrConnectionNotFound if the connection does not exist.
func (s *RedisConnectionStore) Refresh(ctx context.Context, connID string) error {
	if connID == "" {
		return fmt.Errorf("server: connection ID is required")
	}

	infoKey := s.infoKey(connID)

	// Read the info to determine the connection's configured TTL.
	val, err := s.client.Get(ctx, infoKey).Result()
	if err != nil {
		if err == redis.Nil {
			return ErrConnectionNotFound
		}
		return fmt.Errorf("server: refresh connection [connID=%s]: %w", connID, classifyRedisError(err))
	}

	var info ConnectionInfo
	if err := json.Unmarshal([]byte(val), &info); err != nil {
		return fmt.Errorf("server: refresh connection [connID=%s]: unmarshal: %w", connID, err)
	}

	ttl := s.resolveTTL(info.TTL)

	result, err := luaRefresh.Run(ctx, s.client, []string{infoKey},
		fmt.Sprintf("%d", ttl.Milliseconds())).Int()
	if err != nil {
		return fmt.Errorf("server: refresh connection [connID=%s]: %w", connID, classifyRedisError(err))
	}
	if result == 0 {
		return ErrConnectionNotFound
	}

	return nil
}

// ListByUser returns active connections for the given user, up to limit
// results. If limit <= 0, all connections are returned. It uses SScan to
// iterate over the per-user set incrementally, stopping once enough live
// connections have been collected (P2-006). Expired or missing keys are
// silently cleaned up from the set.
func (s *RedisConnectionStore) ListByUser(ctx context.Context, userID string, limit int) ([]*ConnectionInfo, error) {
	if userID == "" {
		return nil, fmt.Errorf("server: user ID is required")
	}

	userKey := s.userKey(userID)

	// Resolve the effective limit. If limit <= 0, fetch all (use a large
	// sentinel that is capped at maxListByUserLimit).
	effectiveLimit := maxListByUserLimit
	if limit > 0 {
		if limit > maxListByUserLimit {
			effectiveLimit = maxListByUserLimit
		} else {
			effectiveLimit = limit
		}
	}

	// Use SScan to iterate the user set incrementally. We fetch in batches
	// and stop once we have enough live connections or the cursor wraps.
	var (
		result     []*ConnectionInfo
		staleIDs   []string
		cursor     uint64
		fetchLimit int64 = 100 // SScan COUNT hint
	)

	for {
		var keys []string
		var err error
		keys, cursor, err = s.client.SScan(ctx, userKey, cursor, "*", fetchLimit).Result()
		if err != nil {
			return nil, fmt.Errorf("server: list connections by user [userID=%s]: %w", userID, classifyRedisError(err))
		}

		if len(keys) > 0 {
			// Build MGET keys for this batch.
			infoKeys := make([]string, 0, len(keys))
			for _, connID := range keys {
				infoKeys = append(infoKeys, s.infoKey(connID))
			}

			vals, err := s.client.MGet(ctx, infoKeys...).Result()
			if err != nil {
				return nil, fmt.Errorf("server: list connections by user [userID=%s]: mget: %w", userID, classifyRedisError(err))
			}

			for i, val := range vals {
				if val == nil {
					staleIDs = append(staleIDs, keys[i])
					continue
				}
				str, ok := val.(string)
				if !ok {
					continue
				}
				var info ConnectionInfo
				if err := json.Unmarshal([]byte(str), &info); err != nil {
					continue
				}
				result = append(result, &info)
			}

			// If we have enough connections, stop scanning.
			if limit > 0 && len(result) >= effectiveLimit {
				if limit > 0 {
					result = result[:effectiveLimit]
				}
				break
			}
		}

		if cursor == 0 {
			break
		}
	}

	// Lazy cleanup: remove stale IDs from the user set.
	if len(staleIDs) > 0 {
		_ = s.client.SRem(ctx, userKey, staleIDs).Err()
	}

	if result == nil {
		return []*ConnectionInfo{}, nil
	}
	return result, nil
}

// CountByUser returns the number of connections in the user's set.
//
// Note: this is an APPROXIMATE count. It uses SCARD which may include
// entries whose info keys have expired but have not yet been cleaned up.
// For an exact count of live connections, use ListByUser and check the
// length of the returned slice (P-006, A-011).
func (s *RedisConnectionStore) CountByUser(ctx context.Context, userID string) (int64, error) {
	if userID == "" {
		return 0, fmt.Errorf("server: user ID is required")
	}

	n, err := s.client.SCard(ctx, s.userKey(userID)).Result()
	if err != nil {
		return 0, fmt.Errorf("server: count connections by user [userID=%s]: %w", userID, classifyRedisError(err))
	}
	return n, nil
}

// CountAll returns the total number of active connections across all users.
// It scans for info keys using the SCAN command (P-010).
//
// This is an approximate count suitable for monitoring and diagnostics.
func (s *RedisConnectionStore) CountAll(ctx context.Context) (int64, error) {
	pattern := s.keyPrefix + redisKeyConnInfoPrefix + "*"
	var count int64
	var cursor uint64

	for {
		keys, nextCursor, err := s.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return 0, fmt.Errorf("server: count all connections: %w", classifyRedisError(err))
		}
		count += int64(len(keys))
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return count, nil
}

// RemoveByUser removes all connections for the given user. It removes both
// the per-connection info keys and the user set itself. Returns the number
// of user-set entries processed. Some info keys may have already expired and
// been evicted by Redis before the deletion, so this number may overcount
// the actual number of info keys deleted (R3-002).
//
// Uses UNLINK instead of DEL (P-008) and processes keys in batches to avoid
// blocking Redis with large deletions.
func (s *RedisConnectionStore) RemoveByUser(ctx context.Context, userID string) (int64, error) {
	if userID == "" {
		return 0, fmt.Errorf("server: user ID is required")
	}

	userKey := s.userKey(userID)

	// Read all connection IDs before deleting the set.
	members, err := s.client.SMembers(ctx, userKey).Result()
	if err != nil {
		return 0, fmt.Errorf("server: remove by user [userID=%s]: list connections: %w",
			userID, classifyRedisError(err))
	}
	if len(members) == 0 {
		return 0, nil
	}

	// Build the list of info keys.
	infoKeys := make([]string, 0, len(members))
	for _, connID := range members {
		infoKeys = append(infoKeys, s.infoKey(connID))
	}

	// Batch delete info keys using UNLINK (P-008) to avoid blocking Redis.
	for i := 0; i < len(infoKeys); i += removeByUserBatchSize {
		end := i + removeByUserBatchSize
		if end > len(infoKeys) {
			end = len(infoKeys)
		}
		batch := infoKeys[i:end]
		if err := s.client.Unlink(ctx, batch...).Err(); err != nil {
			return 0, fmt.Errorf("server: remove by user [userID=%s]: unlink batch: %w",
				userID, classifyRedisError(err))
		}
	}

	// Unlink the user set itself.
	if err := s.client.Unlink(ctx, userKey).Err(); err != nil {
		return 0, fmt.Errorf("server: remove by user [userID=%s]: unlink user set: %w",
			userID, classifyRedisError(err))
	}

	return int64(len(members)), nil
}

// Ping verifies that the Redis backend is reachable.
func (s *RedisConnectionStore) Ping(ctx context.Context) error {
	if err := s.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("server: redis ping: %w", classifyRedisError(err))
	}
	return nil
}

// Close releases resources. If the store was created via
// NewRedisConnectionStore (owning the client), the Redis client is closed.
// If created via NewRedisConnectionStoreFromClient, the client is NOT closed.
func (s *RedisConnectionStore) Close() error {
	if s.ownsClient {
		if err := s.client.Close(); err != nil {
			return fmt.Errorf("server: close redis client: %w", err)
		}
	}
	return nil
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// resolveTTL returns the effective TTL for a connection. If ttl is zero or
// negative, the store's default TTL is used.
func (s *RedisConnectionStore) resolveTTL(ttl time.Duration) time.Duration {
	if ttl > 0 {
		return ttl
	}
	return s.defaultTTL
}

// infoKey returns the Redis key for a connection's info.
func (s *RedisConnectionStore) infoKey(connID string) string {
	return s.keyPrefix + redisKeyConnInfoPrefix + connID
}

// userKey returns the Redis key for a user's connection set.
func (s *RedisConnectionStore) userKey(userID string) string {
	return s.keyPrefix + redisKeyConnUserPrefix + userID
}

// --------------------------------------------------------------------------
// Error classification
// --------------------------------------------------------------------------

// Sentinel errors for Redis-level failures, mirroring the pattern in
// internal/store/errors.go (A-012).
var (
	// ErrRedisConnectionFailed indicates a Redis connection failure.
	ErrRedisConnectionFailed = fmt.Errorf("server: redis connection failed")

	// ErrRedisTimeout indicates a Redis operation timed out.
	ErrRedisTimeout = fmt.Errorf("server: redis timeout")
)

// classifyRedisError translates Redis client errors into server-level errors.
// It matches common Redis error messages for connection failures and timeouts.
func classifyRedisError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()

	// Timeout patterns (checked first so "i/o timeout" is classified as a
	// timeout rather than a connection failure, fixing P2-007/A2-001).
	if strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "i/o timeout") {
		return fmt.Errorf("%w: %v", ErrRedisTimeout, err)
	}

	// Connection failure patterns.
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "dial tcp") {
		return fmt.Errorf("%w: %v", ErrRedisConnectionFailed, err)
	}

	return err
}
