package api

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"github.com/rotisserie/eris"
	"log"
	"net/http"
	"sync"
)

var upgrader = websocket.Upgrader{
	Error: func(w http.ResponseWriter, r *http.Request, status int, reason error) {
		log.Printf("Error upgrade WS connection: {status: %s; reason: %q}", status, reason)
	},
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func Ws(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		err = eris.Wrap(err, "Error upgrade WS connection")
		log.Println(err)
		w.WriteHeader(500)
		w.Write([]byte(err.Error()))
		return
	}
	go wsWorker(conn)
}

type Cmd struct {
	Cmd  string      `json:"cmd"`
	Data interface{} `json:"data"`
}

func wsWorker(conn *websocket.Conn) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmdCh := make(chan *Cmd, 1000)
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			_, rawMsg, err := conn.ReadMessage()
			if err != nil {
				log.Println(eris.Wrap(err, "Error read message"))
				cancel()
				return
			}
			var cmd Cmd
			if err := json.Unmarshal(rawMsg, &cmd); err != nil {
				log.Println(eris.Wrap(err, "Bad command"))
				if err := conn.WriteMessage(websocket.TextMessage, []byte("Bad command")); err != nil {
					log.Println(eris.Wrap(err, "Error send message"))
				}
				continue
			}
			cmdCh <- &cmd
		}
	}()
	go func() {
		defer wg.Done()
		writeMsg := func(msg string) {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
				log.Println(eris.Wrap(err, "Error send message"))
			}
		}
		for {
			select {
			case cmd := <-cmdCh:
				switch cmd.Cmd {
				case "test":
					writeMsg("It's TEST command")
				case "exit":
					writeMsg("Exit")
					conn.Close()
					return
				default:
					writeMsg("Unsupported command")
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	wg.Wait()
}
