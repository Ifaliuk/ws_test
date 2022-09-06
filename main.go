package main

import (
	"context"
	"github.com/gorilla/mux"
	"log"
	"net/http"
	"syscall"
	"time"
	"ws/api"
	"ws/gracefull_shutdown"
)

func main() {
	gs := gracefull_shutdown.NewGracefulShutdown(syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer gs.CancelContext()

	r := mux.NewRouter()
	r.HandleFunc("/ws", api.Ws).Methods("GET")

	go func() {
		srv := http.Server{
			Addr:    ":8000",
			Handler: r,
		}
		gs.AddWaitCallback("stopHttpServer", func(ctx context.Context) error {
			return srv.Shutdown(ctx)
		})
		log.Println("Start http server...")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Error start http server: %v", err)
			gs.CancelContext()
		}
	}()

	<-gs.Wait(time.Second * 15)
	log.Println("Exit...")
}
