package worker

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	DefaultUsername = "username"
)

type ChatWorker struct {
	username string
	reader   *bufio.Reader
}

func (receiver *ChatWorker) ProducePayload() ([]byte, error) {
	input, err := receiver.reader.ReadString('\n')

	if err == io.EOF {
		return nil, ErrFinish
	}

	return []byte(fmt.Sprintf("%v: %v", receiver.username, input)), err
}

func (receiver *ChatWorker) ConsumePayload(bytes []byte) error {
	fmt.Printf("%v\n", string(bytes))

	return nil
}

func NewChatWorker(username string) (*ChatWorker, error) {
	if username == "" {
		return nil, errors.New("username can't be empty")
	}

	return &ChatWorker{
			username: username,
			reader:   bufio.NewReader(os.Stdin)},
		nil
}
