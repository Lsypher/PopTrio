// poptrio 是 PopTrio 对战服务进程入口：单进程对外提供 HTTP 健康检查
// （GET /healthz）与 JSON over WebSocket 对战端点（GET /ws），内含匹配
// 队列与房间 Actor。全部参数为环境变量配置项，缺省即 MVP 配置。
package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"poptrio/server/internal/hub"
	"poptrio/server/internal/room"
)

func main() {
	addr := env("POPTRIO_ADDR", ":8080")
	queueTimeout := envSeconds("POPTRIO_QUEUE_TIMEOUT_SECONDS", 120) // 排队 2 分钟超时
	turnSeconds := envInt("POPTRIO_TURN_SECONDS", 10)
	totalTurns := envInt("POPTRIO_TOTAL_TURNS", 12)
	graceSeconds := envInt("POPTRIO_GRACE_SECONDS", 60)

	cfg := hub.Config{
		Room:         room.DefaultConfig(),
		QueueTimeout: queueTimeout,
	}
	cfg.Room.TurnSeconds = turnSeconds
	cfg.Room.TotalTurns = totalTurns
	cfg.Room.GraceSeconds = graceSeconds

	h := hub.New(cfg, room.Deps{})
	log.Printf("poptrio listening on %s (queue timeout %s, %d turns x %ds)",
		addr, queueTimeout, totalTurns, turnSeconds)
	log.Fatal(http.ListenAndServe(addr, h.Handler()))
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			log.Fatalf("env %s: %v", key, err)
		}
		return n
	}
	return fallback
}

func envSeconds(key string, fallback int) time.Duration {
	return time.Duration(envInt(key, fallback)) * time.Second
}
