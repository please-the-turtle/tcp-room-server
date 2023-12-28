package server

type Message struct {
	text   string
	sender *client
}

func NewMessage(sender *client, text string) *Message {
	return &Message{
		sender: sender,
		text:   text,
	}
}

func (m *Message) String() string {
	return m.text
}
