package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/zlz3907/go-postman"
)

func main() {
	client := postman.NewClient("ws://localhost:7502", "example-client", "your-auth-token-here")

	client.SetRegisterFunc(func() []byte {
		return []byte(`{"type":"register","clientID":"example-client"}`)
	})

	client.SetHandler("welcome", func(msg []byte) {
		fmt.Println("Received welcome message:", string(msg))
	})

	client.SetHandler("msg", func(msg []byte) {
		var message postman.Message
		json.Unmarshal(msg, &message)
		fmt.Printf("Received message from %s: %v\n", message.From, message.Content)
	})

	err := client.Connect()
	if err != nil {
		log.Fatal("Connection error:", err)
	}

	// 每5秒发送一条广播消息
	go func() {
		for {
			time.Sleep(5 * time.Second)
			content := fmt.Sprintf("Hello from example-client at %s", time.Now().Format(time.RFC3339))
			err := client.SendMessageToAll(content)
			if err != nil {
				log.Printf("Error sending message: %v", err)
			}
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the client
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	<-interrupt

	log.Println("Shutting down client...")
}
