package postman

import (
	"crypto/tls"
	"encoding/json"

	// "fmt"
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
	Send           chan Message
	Handlers       map[string]HandlerFunc
	RegisterFunc   func() Message
	IsConnected    bool
	reconnectMutex sync.Mutex
	AuthToken      string
	ClientID       string
	HeartbeatTo    []string
	HeartbeatMDB   interface{}  // 新增：用于存储MDB数据
	heartbeatMu    sync.RWMutex // 新增：用于保护HeartbeatMDB的并发访问
}

type Message struct {
	From    string      `json:"from"`
	To      interface{} `json:"to"`
	Subject string      `json:"subject"`
	Content interface{} `json:"content"`
	Type    string      `json:"type"`
	// 可选字段
	CC             interface{} `json:"cc,omitempty"`
	ContentType    int         `json:"contentType,omitempty"`
	Charset        string      `json:"charset,omitempty"`
	Level          int         `json:"level,omitempty"`
	Tags           []string    `json:"tags,omitempty"`
	Attachments    []string    `json:"attachments,omitempty"`
	References     string      `json:"references,omitempty"`
	InReplyTo      string      `json:"inReplyTo,omitempty"`
	SubjectID      string      `json:"subjectId,omitempty"`
	CreateTime     int64       `json:"createTime,omitempty"`
	LastUpdateTime int64       `json:"lastUpdateTime,omitempty"`
	State          int         `json:"state,omitempty"`
	Token          string      `json:"token,omitempty"`
	FromTag        string      `json:"fromTag,omitempty"`
}

type HandlerFunc func(Message)

func NewClient(urlStr, clientID, authToken string, handlers map[string]HandlerFunc) *Client {
	client := &Client{
		URL:          urlStr,
		Send:         make(chan Message, 256),
		Handlers:     make(map[string]HandlerFunc),
		IsConnected:  false,
		AuthToken:    authToken,
		ClientID:     clientID,
		HeartbeatTo:  []string{},
		HeartbeatMDB: nil,
	}

	// Initialize handlers
	for msgType, handler := range handlers {
		client.SetHandler(msgType, handler)
	}

	return client
}

func (c *Client) Connect() error {
	u, err := url.Parse(c.URL)
	if err != nil {
		return err
	}

	q := u.Query()
	q.Set("clientID", c.ClientID)
	u.RawQuery = q.Encode()

	header := http.Header{}
	header.Add("Authorization", "Bearer "+c.AuthToken)

	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 45 * time.Second,
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
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

		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("JSON unmarshal error: %v", err)
			continue
		}

		if handler, exists := c.Handlers[msg.Type]; exists {
			handler(msg)
		} else if c.Handlers["default"] != nil {
			c.Handlers["default"](msg)
		} else {
			log.Printf("No handler for message type: %s", msg.Type)
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

			jsonMessage, err := json.Marshal(message)
			if err != nil {
				log.Printf("JSON marshal error: %v", err)
				continue
			}

			err = c.Conn.WriteMessage(websocket.TextMessage, jsonMessage)
			if err != nil {
				log.Printf("write error: %v", err)
				return
			}

		case <-ticker.C:
			heartbeat := Message{
				From:    c.ClientID,
				To:      c.HeartbeatTo,
				Subject: "Heartbeat",
				Content: c.GetHeartbeatMDB(), // 使用MDB数据作为心跳内容
				Type:    "heartbeat",
			}

			jsonHeartbeat, err := json.Marshal(heartbeat)
			if err != nil {
				log.Printf("Heartbeat JSON marshal error: %v", err)
				continue
			}

			err = c.Conn.WriteMessage(websocket.TextMessage, jsonHeartbeat)
			if err != nil {
				log.Printf("Heartbeat write error: %v", err)
				return
			}

			log.Println("Heartbeat sent")
		}
	}
}

func (c *Client) SetHandler(msgType string, handler func(Message)) {
	c.Handlers[msgType] = handler
}

func (c *Client) SetRegisterFunc(registerFunc func() Message) {
	c.RegisterFunc = registerFunc
}

func (c *Client) SendMessage(message Message) {
	c.Send <- message
}

func (c *Client) SendMessageToAll(content interface{}) error {
	message := Message{
		From:    c.ClientID,
		To:      []string{},
		Subject: "Broadcast Message",
		Content: content,
		Type:    "msg",
	}

	c.Send <- message
	return nil
}

// 新增：设置心跳消息广播对象的方法
func (c *Client) SetHeartbeatTo(to []string) {
	c.HeartbeatTo = to
}

// 新增：设置心跳MDB数据的方法
func (c *Client) SetHeartbeatMDB(mdb interface{}) {
	c.heartbeatMu.Lock()
	defer c.heartbeatMu.Unlock()
	c.HeartbeatMDB = mdb
}

// 新增：获取心跳MDB数据的方法
func (c *Client) GetHeartbeatMDB() interface{} {
	c.heartbeatMu.RLock()
	defer c.heartbeatMu.RUnlock()
	return c.HeartbeatMDB
}
