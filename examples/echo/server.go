package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/sushil-cmd-r/websocket"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r)
		if err != nil {
			log.Fatal(err)
		}
		defer conn.Close()

		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("read error: %v\n", err)
				break
			}

			if err := conn.WriteMessage(mt, msg); err != nil {
				fmt.Printf("write error: %v\n", err)
				break
			}
		}
	})

	log.Fatal(http.ListenAndServe(":8080", nil))
}
