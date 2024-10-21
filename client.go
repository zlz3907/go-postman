package postman

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	URL            string
	Conn           *websocket.Conn
	Send           chan []byte
	Handlers       map[string]func([]byte)
	RegisterFunc   func() []byte
	IsConnected    bool
	reconnectMutex sync.Mutex
	AuthToken      string
	ClientID       string
}

func NewClient(urlStr, clientID, authToken string) *Client {
	return &Client{
		URL:         urlStr,
		Send:        make(chan []byte, 256),
		Handlers:    make(map[string]func([]byte)),
		IsConnected: false,
		AuthToken:   authToken,
		ClientID:    clientID,
	}
}

func (c *Client) Connect() error {
	u, err := url.Parse(c.URL)
	if err != nil {
		return err
	}

	// 添加 clientID 到 URL 查询参数
	q := u.Query()
	q.Set("clientID", c.ClientID)
	u.RawQuery = q.Encode()

	// 创建自定义的 header
	header := http.Header{}
	header.Add("Authorization", "Bearer "+c.AuthToken)

	// 使用自定义的 Dialer
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 45 * time.Second,
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true}, // 仅用于测试，生产环境应移除
	}

	conn, _, err := dialer.Dial(u.String(), header)
	if err != nil {
		return err
	}

	c.Conn = conn
	c.IsConnected = true

	go c.readPump()
	go c.writePump()

	if c.RegisterFunc != nil {
		c.Send <- c.RegisterFunc()
	}

	return nil
}

func (c *Client) reconnect() {
	c.reconnectMutex.Lock()
	defer c.reconnectMutex.Unlock()

	if c.IsConnected {
		return
	}

	for {
		log.Println("Attempting to reconnect...")
		err := c.Connect()
		if err == nil {
			log.Println("Reconnected successfully")
			return
		}
		log.Printf("Reconnection failed: %v. Retrying in 5 seconds...", err)
		time.Sleep(5 * time.Second)
	}
}

func (c *Client) readPump() {
	defer func() {
		c.IsConnected = false
		c.Conn.Close()
		go c.reconnect()
	}()

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			log.Printf("read error: %v", err)
			return
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("JSON unmarshal error: %v", err)
			continue
		}

		msgType, ok := msg["type"].(string)
		if !ok {
			log.Println("Message type not found or not a string")
			continue
		}

		if handler, exists := c.Handlers[msgType]; exists {
			handler(message)
		} else {
			log.Printf("No handler for message type: %s", msgType)
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			err := c.Conn.WriteMessage(websocket.TextMessage, message)
			if err != nil {
				log.Printf("write error: %v", err)
				return
			}

		case <-ticker.C:
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("ping error: %v", err)
				return
			}
		}
	}
}

func (c *Client) SetHandler(msgType string, handler func([]byte)) {
	c.Handlers[msgType] = handler
}

func (c *Client) SetRegisterFunc(registerFunc func() []byte) {
	c.RegisterFunc = registerFunc
}

func (c *Client) SendMessage(message []byte) {
	c.Send <- message
}

type Message struct {
	From    string      `json:"from"`
	To      interface{} `json:"to"` // 可以是字符串或字符串数组
	Subject string      `json:"subject"`
	Content interface{} `json:"content"` // 可以是任何类型
	Type    string      `json:"type"`    // 消息类型
}

func (c *Client) SendMessageToAll(content interface{}) error {
	message := Message{
		From:    c.ClientID,
		To:      []string{}, // 空数组表示发送给所有人
		Subject: "Broadcast Message",
		Content: content,
		Type:    "msg",
	}

	jsonMessage, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("error marshaling message: %v", err)
	}

	c.Send <- jsonMessage
	return nil
}
