package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"os/exec"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func websocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()

	for {
		// read message from client
		_, message, err := conn.ReadMessage()

		if err != nil {
			log.Println(err)
			break
		}
		// show message
		log.Printf("Received message: %s", message)

		// run command
		websocket_message := string(message[:])
		args := strings.Split(websocket_message, " ")

		cmd := exec.Command(args[0], args[1:]...)

		output, err := cmd.Output()
		if err != nil {
			log.Print("Failed: %v", err)
		}
		fmt.Printf("%s\n", output)

		// b, err := cmd.CombinedOutput()
		// if err != nil {
		// 	log.Print("Failed: %v", err)
		// }

		// fmt.Printf("%s\n", b)

		//send message to client
		//err = conn.WriteMessage(websocket.TextMessage, message)
		err = conn.WriteMessage(websocket.TextMessage, output)
		if err != nil {
			log.Println(err)
			break
		}

	}
}

func main() {
	http.HandleFunc("/websocket", websocketHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
