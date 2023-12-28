package server

import (
	"log"
	"net"
	"strings"
	"sync"

	"github.com/please-the-turtle/tcp-room-server/logging"
)

const (
	TCP_BUFFER_SIZE_BITES = 512

	ERROR_NOTICE_PREFIX           = "ERR: "
	SERVER_FULL_NOTICE            = ERROR_NOTICE_PREFIX + "The server is full"
	CREATING_ROOM_FAILED_NOTICE   = ERROR_NOTICE_PREFIX + "Creating room failed"
	CLIENT_ALREADY_IN_ROOM_NOTICE = ERROR_NOTICE_PREFIX + "Client already in room"
	ROOM_NOT_EXISTS_NOTICE        = ERROR_NOTICE_PREFIX + "Room not exists"
	NOT_A_COMMAND_NOTICE          = ERROR_NOTICE_PREFIX + "Command not exists"
)

var wg sync.WaitGroup

// Server configuration data.
// MaxClients store the maximum number of clients served.
// If maxClients lesser than 1, then number of clients isn't limited.
type ServerConfig struct {
	MaxClients    int
	Port          string
	CommandPrefix string
}

// Provides default server settings.
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		MaxClients:    100,
		Port:          "5558",
		CommandPrefix: ":",
	}
}

// Serves ENet connections and controls sending messages
// between clients inside one room.
type Server struct {
	config   *ServerConfig
	rooms    map[RoomID]*room
	clients  map[ClientID]*client
	handlers map[HandlerPrefix]Handler
	incoming chan *Message
}

// Creates new server.
func NewServer(config *ServerConfig) *Server {
	s := &Server{
		config:   config,
		rooms:    make(map[RoomID]*room),
		clients:  make(map[ClientID]*client),
		handlers: make(map[HandlerPrefix]Handler),
		incoming: make(chan *Message),
	}

	s.handlers[HandlerPrefix("ROOM")] = NewCreateRoomHandler(s)
	s.handlers[HandlerPrefix("JOIN")] = NewJoinRoomHandler(s)
	s.handlers[HandlerPrefix("LEAV")] = NewLeaveRoomHandler(s)

	return s
}

// Adds new client to the server.
func (s *Server) Join(c *client) {
	if len(s.clients) >= s.config.MaxClients {
		c.conn.Write([]byte(SERVER_FULL_NOTICE))
		c.quit()
		return
	}

	s.clients[c.id] = c
	go func() {
		for message := range c.incoming {
			s.incoming <- message
		}
		s.Disconnect(c)
	}()
	c.serve()
	c.outgoing <- NewMessage(c, string(c.id))

	logging.Info("New client joined on server")
}

// Disconnects client from server.
func (s *Server) Disconnect(c *client) {
	s.removeFromRoom(c)
	c.quit()
	delete(s.clients, c.id)
}

func (s *Server) Listen() {
	logging.Info("Server started on port", s.config.Port)
	wg.Add(1)
	go s.listen()
	go func() {
		for {
			message := <-s.incoming
			s.parse(message)
		}
	}()
	wg.Wait()
}

// Parses the message for processing.
// If the message text begins with a command symbol,
// then it tries to select the appropriate handler in accordance with HandlerPrefix.
func (s *Server) parse(m *Message) {
	if !strings.HasPrefix(m.text, s.config.CommandPrefix) {
		s.Send(m)
		return
	}

	handlerPrefix := m.text[len(s.config.CommandPrefix):]
	handlerPrefix, _, _ = strings.Cut(handlerPrefix, " ")
	handlerPrefix = strings.TrimSuffix(handlerPrefix, "\n")
	handler, prs := s.handlers[HandlerPrefix(handlerPrefix)]

	if !prs {
		m.sender.outgoing <- NewMessage(m.sender, NOT_A_COMMAND_NOTICE)
		logging.Errorf("HandlePrefix '%s' incorrect.", handlerPrefix)
		return
	}

	err := handler.handle(m)
	if err != nil {
		m.sender.outgoing <- NewMessage(m.sender, ERROR_NOTICE_PREFIX+err.Error())
		logging.Error(err)
	}
}

// Sends the message to clients from the same room
func (s *Server) Send(m *Message) {
	if m.sender == nil {
		logging.Warning("Message not sent: Client is nil")
		return
	}

	if m.sender.room == nil {
		logging.Warning("Message not sent: Client not in the room")
		return
	}

	m.sender.room.broadcast(m)
}

func (s *Server) CreateRoom(c *client, roomCapacity int) {
	if c.room != nil {
		c.outgoing <- NewMessage(c, CLIENT_ALREADY_IN_ROOM_NOTICE)
		logging.Info("Creating a room: Client already in another room")
		return
	}

	room := NewRoom(roomCapacity)
	if s.rooms[room.id] != nil {
		c.outgoing <- NewMessage(c, CREATING_ROOM_FAILED_NOTICE)
		logging.Errorf("Creating room with id %s failed", room.id)
		return
	}

	s.rooms[room.id] = room
	logging.Infof("New room with id %s created", room.id)
	room.join(c)

	c.outgoing <- NewMessage(c, string(c.room.id))
}

func (s *Server) DeleteRoom(r *room) {
	logging.Infof("Room with id %s deleted.", r.id)
	delete(s.rooms, r.id)
}

func (s *Server) JoinRoom(c *client, roomID RoomID) {
	if c.room != nil {
		c.outgoing <- NewMessage(c, CLIENT_ALREADY_IN_ROOM_NOTICE)
		logging.Infof("Joining to the room: Client already in another room")
		return
	}

	room, prs := s.rooms[roomID]
	if !prs {
		c.outgoing <- NewMessage(c, ROOM_NOT_EXISTS_NOTICE)
		logging.Infof("Joining to the room: Room with id %s not exists", roomID)
		return
	}

	err := room.join(c)
	if err != nil {
		c.outgoing <- NewMessage(c, err.Error())
		return
	}

	c.outgoing <- NewMessage(c, "JOINED")
	logging.Infof("Client %s joined to the room %s", c.id, room.id)
}

func (s *Server) LeaveRoom(c *client) {
	s.removeFromRoom(c)
	c.outgoing <- NewMessage(c, CLIENT_LEFT_ROOM_NOTICE)
}

func (s *Server) removeFromRoom(c *client) {
	room := c.room

	if room == nil {
		return
	}

	room.leave(c)
	logging.Infof("Client with id %s left room %s", c.id, room.id)

	if len(room.members) == 0 {
		s.DeleteRoom(room)
	}
}

func (s *Server) listen() {
	defer wg.Done()

	addr, err := net.ResolveTCPAddr("tcp", ":"+s.config.Port)
	if err != nil {
		logging.Errorf("TCP adderess not resolved: %s", err.Error())
		return
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		logging.Errorf("TCP listener not created: %s", err.Error())
		return
	}
	defer l.Close()

	for {
		conn, err := l.AcceptTCP()
		if err != nil {
			log.Printf("ERROR: Client not accepted: %s", err.Error())
			continue
		}

		client := NewClient(conn)
		s.Join(client)
	}
}
