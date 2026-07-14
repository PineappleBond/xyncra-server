// Command xyncra-server is the entry point for the Xyncra WebSocket server.
//
// It initializes the database, message broker, and connection store, then
// starts the WebSocket server. Configuration is provided via command-line
// flags (which take precedence) or environment variables.
//
// Usage:
//
//	xyncra-server [flags]
//
// Flags:
//
//	-addr          WebSocket server listen address (default ":8080")
//	-redis-addr    Redis server address (default "localhost:6379")
//	-redis-password  Redis AUTH password (default "")
//	-redis-db      Redis database index (default 0)
//	-db-driver     Database driver: sqlite, postgres, mysql (default "sqlite")
//	-db-dsn        Database DSN / connection string (default "xyncra.db")
//	-max-conns     Max connections per user, 0 = unlimited (default 0)
//	-agents-dir    Path to agent config directory (default "agents")
//	-max-functions-per-device  Max functions a device can register (default 200)
//
// Environment variables (used as fallback when flags are not set):
//
//	XYNCRA_ADDR, XYNCRA_REDIS_ADDR, XYNCRA_REDIS_PASSWORD, XYNCRA_REDIS_DB,
//	XYNCRA_DB_DRIVER, XYNCRA_DB_DSN, XYNCRA_MAX_CONNS_PER_USER,
//	XYNCRA_MAX_FUNCTIONS_PER_DEVICE
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/agent"
	agenttools "github.com/PineappleBond/xyncra-server/internal/agent/tools"
	"github.com/PineappleBond/xyncra-server/internal/cleanup"
	"github.com/PineappleBond/xyncra-server/internal/handler"
	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/redis/go-redis/v9"
)

// Version information, injected at build time via -ldflags.
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	// ---------------------------------------------------------------
	// Configuration
	// ---------------------------------------------------------------

	addr := flag.String("addr", envOrDefault("XYNCRA_ADDR", ":8080"),
		"WebSocket server listen address")
	redisAddr := flag.String("redis-addr", envOrDefault("XYNCRA_REDIS_ADDR", "localhost:6379"),
		"Redis server address")
	redisPassword := flag.String("redis-password", os.Getenv("XYNCRA_REDIS_PASSWORD"),
		"Redis AUTH password")
	redisDB := flag.Int("redis-db", envOrDefaultInt("XYNCRA_REDIS_DB", 0),
		"Redis database index")
	dbDriver := flag.String("db-driver", envOrDefault("XYNCRA_DB_DRIVER", "sqlite"),
		"Database driver (sqlite, postgres, mysql)")
	dbDSN := flag.String("db-dsn", envOrDefault("XYNCRA_DB_DSN", "xyncra.db"),
		"Database DSN / connection string")
	maxConns := flag.Int("max-conns", envOrDefaultInt("XYNCRA_MAX_CONNS_PER_USER", 0),
		"Max connections per user (0 = unlimited)")
	agentsDir := flag.String("agents-dir", envOrDefault("XYNCRA_AGENTS_DIR", "agents"),
		"Path to agent config directory")
	maxFunctionsPerDevice := flag.Int("max-functions-per-device",
		envOrDefaultInt("XYNCRA_MAX_FUNCTIONS_PER_DEVICE", 200),
		"Max functions a device can register")
	flag.Parse()

	log.Printf("starting xyncra-server %s (%s) built %s on %s", version, commit, buildTime, *addr)

	// ---------------------------------------------------------------
	// Database / Store
	// ---------------------------------------------------------------

	db, err := store.NewDatabase(store.DatabaseConfig{
		Driver: *dbDriver,
		DSN:    *dbDSN,
	})
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	dataStore := store.NewFromDatabase(db)

	// Run auto-migration to ensure all tables exist.
	migrateCtx, migrateCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := dataStore.AutoMigrate(migrateCtx); err != nil {
		migrateCancel()
		log.Fatalf("failed to auto-migrate database: %v", err)
	}
	migrateCancel()
	log.Println("database migrated successfully")

	// ---------------------------------------------------------------
	// Redis ConnectionStore
	// ---------------------------------------------------------------

	connStore, err := server.NewRedisConnectionStore(server.RedisConnectionStoreConfig{
		Addr:                  *redisAddr,
		Password:              *redisPassword,
		DB:                    *redisDB,
		MaxConnectionsPerUser: *maxConns,
	})
	if err != nil {
		log.Fatalf("failed to create connection store: %v", err)
	}
	defer connStore.Close()

	// ---------------------------------------------------------------
	// Message Broker (Asynq over Redis)
	// ---------------------------------------------------------------

	broker, err := mq.NewAsynqBroker(mq.AsynqConfig{
		RedisAddr:     *redisAddr,
		RedisPassword: *redisPassword,
		RedisDB:       *redisDB,
	})
	if err != nil {
		log.Fatalf("failed to create broker: %v", err)
	}
	defer broker.Close()

	// ---------------------------------------------------------------
	// Node Broadcaster (cross-node message routing via Redis Pub/Sub)
	// ---------------------------------------------------------------

	// Uses a dedicated redis.Client for Pub/Sub since Pub/Sub requires
	// an exclusive connection that cannot be shared with regular commands.
	nodeBroadcasterClient := redis.NewClient(&redis.Options{
		Addr:     *redisAddr,
		Password: *redisPassword,
		DB:       *redisDB,
	})
	nodeBroadcaster := server.NewRedisNodeBroadcaster(nodeBroadcasterClient, "xyncra")
	defer nodeBroadcasterClient.Close()

	// ---------------------------------------------------------------
	// Message Handlers
	// ---------------------------------------------------------------

	msgHandler := server.NewDefaultMessageHandler()

	// ---------------------------------------------------------------
	// Agent Registry
	// ---------------------------------------------------------------

	agentRegistry := agent.NewRegistry()
	if err := agentRegistry.Load(*agentsDir); err != nil {
		log.Printf("warning: failed to load agents from %s: %v", *agentsDir, err)
	}
	log.Printf("loaded %d agent configuration(s)", agentRegistry.Count())

	// ---------------------------------------------------------------
	// Function Registry (D-099)
	// ---------------------------------------------------------------

	funcRegistry := server.NewMemoryFunctionRegistry(server.FunctionRegistryConfig{
		MaxFunctionsPerDevice: *maxFunctionsPerDevice,
	})

	// ---------------------------------------------------------------
	// Redis client for agent pipeline and pending store (D-074)
	// ---------------------------------------------------------------

	// Dedicated redis.Client for agent idempotency, conversation lock,
	// checkpoint store, and pending store (D-074).
	redisIdempotencyClient := redis.NewClient(&redis.Options{
		Addr:     *redisAddr,
		Password: *redisPassword,
		DB:       *redisDB,
	})
	defer redisIdempotencyClient.Close()

	// PendingStore for reverse-RPC request persistence (Phase 4, D-103).
	// Reuses the same dedicated redis.Client as idempotency (D-074).
	pendingStore := server.NewRedisPendingStore(redisIdempotencyClient, server.PendingStoreConfig{})

	// ---------------------------------------------------------------
	// WebSocket Server
	// ---------------------------------------------------------------

	srv, err := server.NewWebSocketServer(
		server.WSWithAddr(*addr),
		server.WSWithConnectionStore(connStore),
		server.WSWithStore(dataStore),
		server.WSWithBroker(broker),
		server.WSWithMessageHandler(msgHandler),
		server.WSWithNodeBroadcaster(nodeBroadcaster),
		server.WSWithFunctionRegistry(funcRegistry),
		server.WSWithPendingStore(pendingStore), // Phase 4 (D-103)
	)
	if err != nil {
		log.Fatalf("failed to create websocket server: %v", err)
	}

	// Wire structured logger into agent registry (Phase 7 review).
	agentRegistry.SetLogger(srv.Logger())

	// Register method handlers after srv is created so that BroadcastFn
	// can reference srv.BroadcastUpdates.
	handler.RegisterAll(msgHandler, handler.Dependencies{
		ConnStore:        connStore,
		Store:            dataStore,
		Broker:           broker,
		BroadcastFn:      srv.BroadcastUpdates,
		AgentRegistry:    agentRegistry,
		FunctionRegistry: funcRegistry,
		ReverseRPC:       srv.ReverseRPC(), // Phase 5 (D-108)
		Logger:           srv.Logger(),     // Phase 5 (D-108)
	})

	// ---------------------------------------------------------------
	// Agent Execution Pipeline
	// ---------------------------------------------------------------

	// LLM call logger — dedicated file for LLM request/response observability.
	// Logs are written in JSONL format (one JSON record per line).
	// Opt-in via XYNCRA_LLM_LOG_DIR; when unset, no file is opened (zero overhead).
	var llmLogger *agent.LLMLogger
	if llmLogDir := os.Getenv("XYNCRA_LLM_LOG_DIR"); llmLogDir != "" {
		if err := os.MkdirAll(llmLogDir, 0755); err == nil {
			logPath := filepath.Join(llmLogDir, "llm-calls.log")
			f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				log.Printf("[WARN] failed to open LLM log file %s: %v", logPath, err)
			} else {
				llmLogger = agent.NewLLMLogger(f, false)
				defer f.Close()
				log.Printf("[INFO] LLM call logging enabled: %s", logPath)
			}
		} else {
			log.Printf("[WARN] failed to create LLM log dir %s: %v", llmLogDir, err)
		}
	}

	llmFactory := agent.NewLLMClientFactory()
	agentBuilder := agent.NewAgentBuilder(llmFactory)
	if llmLogger != nil {
		agentBuilder.SetLLMLogger(llmLogger)
	}
	// Wire the default tool registry (D-078). Built-in tools are registered
	// via init() in the tools package; custom tools can be added here.
	agentBuilder.SetToolRegistry(agenttools.DefaultRegistry)
	// Wire the agent registry for sub-agent resolution (D-081).
	agentBuilder.SetRegistry(agentRegistry)
	streamBridge := agent.NewStreamBridge()
	broadcastHelper := agent.NewBroadcastHelper(srv, srv.Logger())
	contextManager := agent.NewDBContextManager(dataStore.MessageStore())

	llmMetrics := agent.NewLogMetrics(srv.Logger())

	// Checkpoint store for HITL support (D-083).
	// Reuses the same dedicated redis.Client as idempotency.
	// Created early so it can be passed to the executor for cleanup (D-112).
	checkpointStore := agent.NewRedisCheckPointStore(redisIdempotencyClient, "", 0) // defaults: prefix="agent:checkpoint:", ttl=24h

	agentExecutor := agent.NewAgentExecutor(
		agentRegistry,
		contextManager,
		agentBuilder,
		streamBridge,
		broadcastHelper,
		dataStore,
		10, // maxConcurrent: limit parallel LLM calls
		srv.Logger(),
		agent.WithLLMMetrics(llmMetrics),
		agent.WithCheckPointStore(checkpointStore), // D-112: checkpoint cleanup after resume
	)

	// Idempotency store for agent task deduplication (D-Phase5-2).
	// Reuses the dedicated redis.Client created earlier (D-074).
	idempotencyStore := agent.NewRedisIdempotencyStore(redisIdempotencyClient)

	// Conversation lock for per-conversation serialization (D-075).
	// Reuses the same dedicated redis.Client as idempotency (D-074).
	conversationLock := agent.NewRedisConversationLock(redisIdempotencyClient)

	// Wire checkpoint store into agent builder (D-083).
	// The store itself was created earlier alongside the executor (D-112).
	agentBuilder.SetCheckPointStore(checkpointStore)

	// MCP Bridge for external tool servers (D-086).
	// Connections are established lazily during Agent Build; CloseAll during
	// shutdown releases all MCP client resources.
	mcpBridge := agenttools.NewMCPBridge(nil) // nil → uses log.Default()
	agentBuilder.SetMCPBridge(mcpBridge)

	// Wire client function provider and caller for DynamicToolProvider (Phase 6 / D-101).
	// funcRegistry (*server.MemoryFunctionRegistry) satisfies ClientFunctionProvider.
	// srv (*server.WebSocketServer) satisfies ClientCaller via ServerRequest().
	agentBuilder.SetClientFunctionProvider(funcRegistry)
	agentBuilder.SetClientCaller(srv)

	// ---------------------------------------------------------------
	// Context & signal handling
	// ---------------------------------------------------------------

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("received signal %v, shutting down...", sig)
		cancel()
	}()

	// Start the broker worker pool in a background goroutine.
	// Tasks that are not yet handled are logged and acknowledged.
	taskHandler := mq.NewTaskHandler()
	taskHandler.Register(mq.TypeSendMessage,
		handler.NewSendMessageTaskHandler(srv.BroadcastUpdates, srv.Logger()))

	// Register agent task handler (Phase 5).
	agentTaskHandler := agent.NewAgentTaskHandler(agentExecutor, idempotencyStore, conversationLock, srv.Logger())
	taskHandler.Register(mq.TypeAgentProcess, agentTaskHandler)

	// Register agent resume handler (Phase 8B / D-085).
	agentResumeHandler := agent.NewAgentResumeHandler(agentExecutor, agentRegistry, conversationLock, srv.Logger())
	taskHandler.Register(mq.TypeAgentResume, agentResumeHandler)

	go func() {
		if err := broker.Start(ctx, taskHandler); err != nil {
			log.Printf("broker error: %v", err)
		}
	}()

	// Start the context cache cleanup goroutine (D-060).
	// Periodically removes expired in-memory conversation context cache entries.
	go contextManager.StartCleanup(ctx, 5*time.Minute)

	// Start cleanup of expired tool results (D-080).
	go agenttools.DefaultToolResultStore.StartCleanup(ctx, 5*time.Minute)

	// Start the UserUpdate cleanup goroutine.
	// Periodically removes expired UserUpdate records (older than 30 days).
	// Uses default 1-hour interval per D-001 zero-config philosophy.
	cleaner := cleanup.NewUserUpdateCleaner(cleanup.Config{
		Store: dataStore.UserUpdateStore(),
	})
	go cleaner.Run(ctx)

	// ---------------------------------------------------------------
	// Run
	// ---------------------------------------------------------------

	if err := srv.Start(ctx); err != nil {
		log.Fatalf("server error: %v", err)
	}

	// ---------------------------------------------------------------
	// Graceful shutdown
	// ---------------------------------------------------------------

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	if err := srv.GracefulStop(stopCtx); err != nil {
		log.Printf("graceful stop error: %v", err)
	}

	// Close all MCP server connections after in-flight requests finish (D-086).
	mcpBridge.CloseAll()

	log.Println("server stopped")
}

// envOrDefault returns the value of the environment variable identified by
// key, or fallback if the variable is empty or unset.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envOrDefaultInt returns the integer value of the environment variable
// identified by key, or fallback if the variable is empty, unset, or cannot
// be parsed as an integer.
func envOrDefaultInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: invalid integer %q for env %s, using default %d\n", v, key, fallback)
		return fallback
	}
	return n
}
