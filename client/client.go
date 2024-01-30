package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"github.com/c-bata/go-prompt"
)

func main() {
	conn, err := net.Dial("tcp", "127.0.0.1:2020")
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()
	log.Println("请在最前面添加 'gemini:' 来询问 Google AI gemini")

	done := make(chan struct{})

	// 接收消息
	go func() {
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			fmt.Printf("\r%s\n>>> ", scanner.Text())
		}
		if scanner.Err() != nil {
			log.Fatalf("Failed to read from server: %v", scanner.Err())
		}
		done <- struct{}{}
	}()

	// 发送消息
	go func() {
		p := prompt.New(
			func(in string) {
				in = strings.TrimSpace(in)
				if in == "quit" || in == "exit" {
					fmt.Println("退出聊天室...")
					conn.Close()
					os.Exit(0)
					return
				}
				if in == "" {
					return
				}
				_, err := conn.Write([]byte(in + "\n"))
				if err != nil {
					log.Fatalf("Failed to write to server: %v", err)
				}
			},
			func(d prompt.Document) []prompt.Suggest {
				// TODO 根据情况添加自动完成的建议
				return []prompt.Suggest{}
			},
			prompt.OptionPrefix(">>> "),
			prompt.OptionPrefixTextColor(prompt.Yellow),
		)
		p.Run()
	}()

	<-done
}
