package api

import (
	"fmt"
	"ha-switcher/internal/db"
	"ha-switcher/internal/switcher"
	"log"
	"net/http"
)

// Server 提供HTTP API来控制系统
type Server struct {
	dbManager *db.DBManager
	switcher  *switcher.Switcher
	port      int
}

// NewServer 创建一个新的API服务器
func NewServer(dbManager *db.DBManager, sw *switcher.Switcher, port int) *Server {
	return &Server{
		dbManager: dbManager,
		switcher:  sw,
		port:      port,
	}
}

// Start 启动HTTP服务器
func (s *Server) Start() error {
	// 故障模拟API
	http.HandleFunc("/api/simulate-failure", func(w http.ResponseWriter, r *http.Request) {
		enable := r.URL.Query().Get("enable")
		if enable == "true" {
			s.dbManager.SetSimulateFailure(true)
			fmt.Fprintf(w, "Master failure simulation enabled\n")
		} else if enable == "false" {
			s.dbManager.SetSimulateFailure(false)
			fmt.Fprintf(w, "Master failure simulation disabled\n")
		} else {
			fmt.Fprintf(w, "Usage: /api/simulate-failure?enable=true|false\n")
		}
	})

	// 状态API
	http.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		count, lastTime := s.switcher.GetSwitchStats()
		fmt.Fprintf(w, "Switch count: %d\nLast switch: %v\n", count, lastTime)
	})

	// 帮助API
	http.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "MySQL HA Switcher API\n")
		fmt.Fprintf(w, "Available endpoints:\n")
		fmt.Fprintf(w, "  /api/simulate-failure?enable=true|false - Control failure simulation\n")
		fmt.Fprintf(w, "  /api/status - Show switcher status\n")
	})

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("Starting HTTP server at http://localhost%s", addr)
	return http.ListenAndServe(addr, nil)
}
