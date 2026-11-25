package wiring

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	taskservice "researchpilot/internal/task"
	"time"

	"github.com/cloudwego/hertz/pkg/route"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"researchpilot/internal/app"
	"researchpilot/internal/common/envconfig"
	"researchpilot/internal/execution"
	"researchpilot/internal/handler"
	"researchpilot/internal/outbound"
	"researchpilot/internal/persistence"
	"researchpilot/internal/repository"
	dbmodel "researchpilot/internal/repository/model"
	"researchpilot/internal/service"
	toolcontract "researchpilot/internal/tools"
	researchtools "researchpilot/internal/tools/research"
)

type appConfig struct{ Origin, WebDir, RedisAddr, RedisPassword, Queue string }

func readConfig() appConfig {
	return appConfig{Origin: envconfig.String("PUBLIC_ORIGIN", "http://localhost:8080"), WebDir: envconfig.String("WEB_DIR", "web/dist"), RedisAddr: envconfig.String("REDIS_ADDR", "127.0.0.1:6379"), RedisPassword: os.Getenv("REDIS_PASSWORD"), Queue: envconfig.String("RESEARCH_QUEUE", "rp_local_research")}
}
func provideDatabase(ctx context.Context) (*sql.DB, func(), error) {
	db, err := persistence.OpenMySQL(ctx, os.Getenv("MYSQL_DSN"))
	if err != nil {
		return nil, nil, err
	}
	return db, func() { _ = db.Close() }, nil
}
func provideRedis(cfg appConfig) (*redis.Client, func()) {
	client := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, ContextTimeoutEnabled: true})
	return client, func() { _ = client.Close() }
}
func provideLimiter(client *redis.Client) handler.RateLimiter {
	return persistence.NewRedisLimiter(client, envconfig.String("APP_ENV", "local"))
}
func provideHealth(db *sql.DB, client *redis.Client, store *repository.Store) handler.Health {
	return handler.Health{
		Pinger:          db,
		PingRedis:       func(ctx context.Context) error { return client.Ping(ctx).Err() },
		HasDelayedTasks: store.HasDelayedTasks,
	}
}
func provideAuth(store *repository.Store, limiter handler.RateLimiter, cfg appConfig) handler.Auth {
	return handler.Auth{Store: store, Guests: store, Origin: cfg.Origin, Limiter: limiter}
}

type modelClient struct{ *http.Client }
type toolsClient struct{ *http.Client }

func provideModelClient() (modelClient, func()) {
	client := service.ModelHTTPClient()
	return modelClient{client}, client.CloseIdleConnections
}
func provideToolsClient() (toolsClient, func()) {
	client := outbound.Client(30*time.Second, true)
	return toolsClient{client}, client.CloseIdleConnections
}
func provideModelService(store *repository.Store, client modelClient) service.Models {
	return service.Models{Store: store, HTTPClient: client.Client}
}
func provideModels(models service.Models, auth handler.Auth) handler.Models {
	return handler.Models{Service: models, Auth: auth}
}
func provideTaskService(store *repository.Store) taskservice.Tasks {
	return taskservice.Tasks{Store: store}
}
func provideTaskActions(store *repository.Store) taskservice.TaskActions {
	return taskservice.TaskActions{Store: store}
}
func provideEventStream(store *repository.Store) taskservice.EventStream {
	return taskservice.EventStream{Store: store}
}
func provideTasks(tasks taskservice.Tasks, actions taskservice.TaskActions, stream taskservice.EventStream, auth handler.Auth) handler.Tasks {
	return handler.Tasks{Service: tasks, Actions: actions, Stream: stream, Auth: auth}
}
func provideWeb(cfg appConfig) handler.Web { return handler.Web{Root: cfg.WebDir} }

type apiComponents struct {
	Health handler.Health
	Auth   handler.Auth
	Models handler.Models
	Tasks  handler.Tasks
	Web    handler.Web
}

func (a apiComponents) register(r route.IRouter) {
	a.Health.RegisterRoutes(r)
	a.Auth.RegisterRoutes(r)
	a.Models.RegisterRoutes(r)
	a.Tasks.RegisterRoutes(r)
	a.Web.RegisterRoutes(r)
}
func provideAPI(health handler.Health, auth handler.Auth, models handler.Models, tasks handler.Tasks, web handler.Web) apiComponents {
	return apiComponents{health, auth, models, tasks, web}
}
func provideToolRegistry(client toolsClient) *toolcontract.Registry[[]dbmodel.Source] {
	return researchtools.NewRegistry(client.Client)
}
func provideRunner(store *repository.Store, model execution.ReportProvider, tool execution.ResearchTool, modelHTTP modelClient, registry *toolcontract.Registry[[]dbmodel.Source]) execution.Runner {
	return execution.NewRunner(store, model, tool, modelHTTP.Client, registry)
}
func provideAsynqConfig(cfg appConfig) asynq.RedisClientOpt {
	return asynq.RedisClientOpt{Addr: cfg.RedisAddr, Password: cfg.RedisPassword}
}
func provideAsynqClient(opt asynq.RedisClientOpt) (*asynq.Client, func()) {
	c := asynq.NewClient(opt)
	return c, func() { _ = c.Close() }
}

func provideAsynqServer(opt asynq.RedisClientOpt, cfg appConfig) *asynq.Server {
	return asynq.NewServer(opt, asynq.Config{Concurrency: 4, Queues: map[string]int{cfg.Queue: 1}, ShutdownTimeout: 8 * time.Second})
}
func provideWorker(store *repository.Store, runner execution.Runner, client *asynq.Client, server *asynq.Server, cfg appConfig) app.WorkerComponents {
	return app.WorkerComponents{Store: store, Runner: runner, Client: client, Server: server, Queue: cfg.Queue}
}
