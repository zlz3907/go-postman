package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/zlz3907/go-postman"
)

// MDB 结构体示例
type MDB struct {
	Temperature float64 `json:"temperature"`
	Humidity    float64 `json:"humidity"`
	Timestamp   int64   `json:"timestamp"`
}

func main() {
	client := postman.NewClient("wss://socket.zhycit.com", "/service/socket/jyiai/api/001", "your-auth-token-here")

	// 设置心跳消息的广播对象
	client.SetHeartbeatTo([]string{"server", "otherClient"})

	// 初始化MDB数据
	mdb := &MDB{
		Temperature: 25.5,
		Humidity:    60.0,
		Timestamp:   time.Now().Unix(),
	}
	client.SetHeartbeatMDB(mdb)

	client.SetRegisterFunc(func() postman.Message {
		return postman.Message{
			From:    "example-client",
			To:      "",
			Subject: "Registration",
			Content: "Registering client",
			Type:    "register",
		}
	})

	client.SetHandler("welcome", func(msg postman.Message) {
		fmt.Printf("Received welcome message: %v\n", msg.Content)
	})

	client.SetHandler("msg", func(msg postman.Message) {
		fmt.Printf("Received message from %s: %v\n", msg.From, msg.Content)
	})

	client.SetHandler("heartbeat", func(msg postman.Message) {
		fmt.Println("Received heartbeat response from server")
	})

	err := client.Connect()
	if err != nil {
		log.Fatal("Connection error:", err)
	}

	// 每25秒发送一条广播消息并更新MDB数据
	go func() {
		for {
			time.Sleep(25 * time.Second)
			content := fmt.Sprintf("Hello from example-client at %s", time.Now().Format(time.RFC3339))
			err := client.SendMessageToAll(content)
			if err != nil {
				log.Printf("Error sending message: %v", err)
			}

			// 更新MDB数据
			newMDB := &MDB{
				Temperature: 25.5 + float64(time.Now().Second()%10), // 模拟温度变化
				Humidity:    60.0 + float64(time.Now().Second()%20), // 模拟湿度变化
				Timestamp:   time.Now().Unix(),
			}
			client.SetHeartbeatMDB(newMDB)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the client
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	<-interrupt

	log.Println("Shutting down client...")
}
