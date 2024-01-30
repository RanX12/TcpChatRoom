package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

type User struct {
	ID             int
	NickName       string
	Addr           string
	EnterAt        time.Time
	MessageChannel chan string
}

// 定义一个 idCounter，用户保护 id 唯一
var (
	nextId    int
	idCounter sync.Mutex
)

var (
	// 新用户到来，通过该 channel 进行登记
	enteringChannel = make(chan *User)
	// 用户离开，通过该 channel 进行登记
	leavingChannel = make(chan *User)
	// 广播专用的用户普通消息 channel，缓冲是尽可能避免出现异常情况堵塞，这里简单给了 8，具体值根据情况调整
	messageChannel = make(chan string, 8)
	geminiKey      string
)

func main() {
	// 不填 IP 就会绑定到当前机器所有的 IP 上
	// 0.0.0.0 同一个网络内任意 PC 都可访问
	listener, err := net.Listen("tcp", "0.0.0.0:2020")
	if err != nil {
		panic(err)
	}

	// 从本地读取环境变量
	godotenv.Load()

	geminiKey = os.Getenv("GEMINI_PRO_API_KEY")

	log.Println("服务已启动！")

	go broadcaster()

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Panicln(err)
			continue
		}

		go handleConn(conn)
	}
}

// broadcaster 用于记录聊天室用户，并进行消息广播：
// 1. 新用户进来；2. 用户普通消息；3. 用户离开
// 这里关键有 3 点：
// 负责登记/注销用户，通过 map 存储在线用户；
// 用户登记、注销，使用专门的 channel。在注销时，除了从 map 中删除用户，还将 user 的 MessageChannel 关闭，避免上文提到的 goroutine 泄露问题；
// 全局的 messageChannel 用来给聊天室所有用户广播消息；
func broadcaster() {
	users := make(map[*User]struct{})

	for {
		select {
		case user := <-enteringChannel:
			// 新用户进入
			users[user] = struct{}{}
		case user := <-leavingChannel:
			// 用户离开
			delete(users, user)
			// 避免 goroutine 泄露
			close(user.MessageChannel)
		case msg := <-messageChannel:
			// 给所有在线用户发送消息
			for user := range users {
				user.MessageChannel <- msg
			}
		}
	}
}

func handleConn(conn net.Conn) {
	defer conn.Close()

	// 1. 新用户进来，构建该用户的实例
	user := &User{
		ID:             genUserID(),
		Addr:           conn.RemoteAddr().String(),
		EnterAt:        time.Now(),
		MessageChannel: make(chan string, 8),
	}

	// 2. 当前在一个新的 goroutine 中，用来进行读操作，因此需要开一个 goroutine 用于写操作
	// 读写 goroutine 之间可以通过 channel 进行通信
	go sendMessage(conn, user.MessageChannel)

	// 3. 给当前用户发送欢迎信息
	// 同时给聊天室所有用户发送有新用户到来的提醒；
	user.MessageChannel <- "请输入你的昵称："
	nickName := bufio.NewScanner(conn)
	if nickName.Scan() {
		user.NickName = nickName.Text()
		user.MessageChannel <- "欢迎你的到来：" + user.NickName
		messageChannel <- "user:`" + user.NickName + "` has enter"
	} else {
		return
	}

	// 4. 将该记录到全局的用户列表中，避免用锁
	// 注意，这里和 3）的顺序不能反，否则自己会收到自己到来的消息提醒；（当然，我们也可以做消息过滤处理）
	enteringChannel <- user

	// 5. 循环读取用户的输入
	input := bufio.NewScanner(conn)
	for input.Scan() {
		if strings.HasPrefix(input.Text(), "gemini:") {
			rep := GeminiChatComplete(input.Text())
			user.MessageChannel <- rep
		} else {
			messageChannel <- user.NickName + ": " + input.Text()
		}
	}

	if err := input.Err(); err != nil {
		log.Println("读取错误：", err)
	}

	// 6. 用户离开
	leavingChannel <- user
	messageChannel <- "user:`" + user.NickName + "` has left"
}

func genUserID() int {
	idCounter.Lock()
	defer idCounter.Unlock()

	nextId++
	return nextId
}

// channel 实际上有三种类型，大部分时候，我们只用了其中一种，就是正常的既能发送也能接收的 channel。
// 除此之外还有单向的 channel：只能接收（<-chan，only receive）和只能发送（chan<-， only send）。
// 它们没法直接创建，而是通过正常（双向）channel 转换而来（会自动隐式转换）。
// 它们存在的价值，主要是避免 channel 被乱用。上面代码中 ch <-chan string 就是为了限制在 sendMessage 函数中只从 channel 读数据，不允许往里写数据。
func sendMessage(conn net.Conn, ch <-chan string) {
	for msg := range ch {
		fmt.Fprintln(conn, msg)
	}
}
