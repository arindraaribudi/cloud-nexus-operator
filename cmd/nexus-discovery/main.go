package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type nexusInfo struct {
	GatewayFQDN string `json:"gatewayFQDN"`
	GatewayPort int32  `json:"gatewayPort"`
	ServerName  string `json:"serverName"`
	Namespace   string `json:"namespace"`
}

func newHandler(info nexusInfo) http.Handler {
	body, err := json.Marshal(info)
	if err != nil {
		panic(fmt.Sprintf("marshal nexusInfo: %v", err))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/nexus-info", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
	return mux
}

func parsePort(s string) int32 {
	var p int32
	_, _ = fmt.Sscanf(s, "%d", &p)
	return p
}

func main() {
	port := os.Getenv("DISCOVERY_PORT")
	if port == "" {
		port = "7001"
	}
	info := nexusInfo{
		GatewayFQDN: os.Getenv("GATEWAY_FQDN"),
		GatewayPort: parsePort(os.Getenv("GATEWAY_PORT")),
		ServerName:  os.Getenv("SERVER_NAME"),
		Namespace:   os.Getenv("SERVER_NAMESPACE"),
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: newHandler(info),
	}

	go func() {
		log.Printf("nexus-discovery listening on :%s", port)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("ListenAndServe: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
