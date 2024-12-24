package main

import (
	"fmt"

	"github.com/sushil-cmd-r/websocket"
)

func main() {
	conn, err := websocket.Dial("ws://localhost:8080")
	if err != nil {
		fmt.Printf("connection error: %v", err)
		return
	}

	defer conn.Close()
	for {
		fmt.Print("> ")
		var input string
		_, err := fmt.Scanln(&input)
		if err != nil {
			fmt.Printf("scan error: %v", err)
			return
		}

		if input == "exit" {
			fmt.Println("exiting...")
			conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			break
		}

		if err := conn.WriteMessage(websocket.TextMessage, []byte(input)); err != nil {
			fmt.Printf("client write error: %v", err)
			break
		}

		mt, msg, err := conn.ReadMessage()
		if err != nil {
			fmt.Printf("client read error: %v", err)
			break
		}
		fmt.Printf("server message: Type: %d, Msg: %s\n", mt, msg)
	}
}
