package server

import (
	"bufio"
	"io"
	"math/rand"
	"net"
	"strings"

	"github.com/please-the-turtle/tcp-room-server/logging"
)

const (
	CLIENT_ID_ALPHABET = "AaBbCcDdEeFfGgHhIiJjKkLlMmNnOoPpQqRrSsTtUuVvWwXxYyZz0123456789"
	CLIENT_ID_LENGHT   = 16
)

// Unique client id.
type ClientID string

// client implements the interaction of a separate client connected to the server.
type client struct {
	id       ClientID
	room     *room
	conn     net.Conn
	incoming chan *Message
	outgoing chan *Message
	reader   *bufio.Reader
	writer   *bufio.Writer
}

// Creates new client.
func NewClient(conn net.Conn) *client {
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	id := genClientID()

	client := &client{
		id:       id,
		room:     nil,
		conn:     conn,
		incoming: make(chan *Message),
		outgoing: make(chan *Message),
		reader:   reader,
		writer:   writer,
	}

	return client
}

// Starts read and write data from client connection.
func (c *client) serve() {
	go c.readLoop()
	go c.writeLoop()
}

func (c *client) quit() {
	c.conn.Close()
	logging.Infof("The client %s has left the server", c.id)
}

// Reads data from tcp client connection.
func (c *client) readLoop() {
	for {
		str, err := c.reader.ReadString('\n')
		str = strings.Trim(str, " \t\n")

		if err == io.EOF {
			break
		}

		if err != nil {
			logging.Error("Client reader:", err)
			break
		}

		message := NewMessage(c, str)
		c.incoming <- message
	}
	close(c.incoming)
}

// Writes data to the client connection.
func (c *client) writeLoop() {
	for message := range c.outgoing {
		_, err := c.writer.WriteString(message.text + "\n")
		if err != nil {
			logging.Error("Client writer:", err, message.text)
			return
		}

		err = c.writer.Flush()
		if err != nil {
			logging.Error("Client writer:", err, message.text)
			return
		}
	}
	close(c.outgoing)
}

// Generates new ClientID
func genClientID() ClientID {
	b := make([]byte, CLIENT_ID_LENGHT)
	for i := range b {
		b[i] = CLIENT_ID_ALPHABET[rand.Intn(len(CLIENT_ID_ALPHABET))]
	}

	return ClientID(b)
}
