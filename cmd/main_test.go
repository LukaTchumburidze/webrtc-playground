package cmd

import (
	"crypto/tls"
	"fmt"
	irc "github.com/fluffle/goirc/client"
	"log"
	"os"
	"os/signal"
	"testing"
	"webrtc-playground/internal/operator/coordinator"
)

func TestSmth(t *testing.T) {
	name := "webrtc-bot-" + coordinator.RandSeq(5)
	channelName := "#lt-webrtc-playground"

	cfg := irc.NewConfig(name)
	cfg.SSL = true
	cfg.SSLConfig = &tls.Config{ServerName: "irc.freenode.net"}
	cfg.Server = "irc.freenode.net:7000"
	cfg.NewNick = func(n string) string { return n + "^" }
	c := irc.Client(cfg)
	c.EnableStateTracking()

	// Add handlers to do things here!
	// e.g. join a channel on connect.
	c.HandleFunc(irc.CONNECTED,
		func(conn *irc.Conn, line *irc.Line) { conn.Join(channelName) })
	// And a signal on disconnect
	disconnect := make(chan bool)
	quit := make(chan os.Signal)
	c.HandleFunc(irc.DISCONNECTED,
		func(conn *irc.Conn, line *irc.Line) { disconnect <- true })

	c.HandleFunc(irc.ACTION, func(conn *irc.Conn, line *irc.Line) {
		log.Printf("ACTION: %s", line.Text())
	})

	c.HandleFunc(irc.AUTHENTICATE, func(conn *irc.Conn, line *irc.Line) {
		log.Printf("AUTHENTICATE: %s", line.Text())
	})

	c.HandleFunc("352",
		func(c *irc.Conn, l *irc.Line) {
			// The nick is the 7th parameter in the WHO reply.
			otherName := l.Args[5]
			if otherName != name {
				log.Println(otherName, "is online, sending private msg")
			}
			c.Privmsg(otherName, "Hello there")
		})

	// Received a END OF WHO reply.
	c.HandleFunc("315",
		func(c *irc.Conn, l *irc.Line) {
			log.Println("End of /WHO list.")
		})

	c.HandleFunc(irc.PRIVMSG,
		func(conn *irc.Conn, line *irc.Line) {
			if line.Args[1] == "list" {
				c.Who(channelName)
				log.Println("list keyword")
			}
			log.Printf("PRIVMSG: %s : %s ", line.Nick, line.Args)
		})

	c.HandleFunc(irc.JOIN, func(conn *irc.Conn, line *irc.Line) {
		c.Mode(name, "-R")
		log.Printf("JOIN: %s", line)
	})
	c.HandleFunc(irc.WHO, func(conn *irc.Conn, line *irc.Line) {
		log.Printf("WHO: %s", line.Args)
	})

	// Tell client to connect.
	if err := c.Connect(); err != nil {
		fmt.Printf("Connection error: %s\n", err.Error())
	}
	signal.Notify(quit, os.Interrupt, os.Kill)

	for {
		select {
		case sig := <-quit:
			log.Printf("Received a %s signal. Shutting down...\n", sig)
			close(disconnect)
		case <-disconnect:
			log.Printf("Disconnected\n")
			err := c.Close()
			if err != nil {
				return
			}
			return
		}
	}
}
