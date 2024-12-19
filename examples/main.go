package main

import (
	"log"
	"net/http"

	"github.com/sushil-cmd-r/websocket"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r)
		if err != nil {
			log.Println(err)
			return
		}

		defer func() {
			// conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, ""))
			conn.Close()
		}()

		for {
			mt, msg, err := conn.ReadMessage()
			// log.Printf("got message %v, type %v\n", len(msg), mt)
			if err != nil {
				log.Println(err)
				break
			}

			if err := conn.WriteMessage(mt, msg); err != nil {
				log.Println("write error:", err)
				break
			}
		}

	})

	log.Println("starting server...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
