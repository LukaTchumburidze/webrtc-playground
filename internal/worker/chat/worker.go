package chat

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"webrtc-playground/internal/worker"
)

const (
	DEFAULT_USERNAME = "username"
)

type ChatWorker struct {
	username string
	reader   *bufio.Reader
}

func (receiver *ChatWorker) ProducePayload() ([]byte, error) {
	input, err := receiver.reader.ReadString('\n')

	if err == io.EOF {
		return nil, worker.ErrFinish
	}

	return []byte(fmt.Sprintf("%v: %v", receiver.username, input)), err
}

func (receiver *ChatWorker) ConsumePayload(bytes []byte) error {
	fmt.Printf("%v\n", string(bytes))

	return nil
}

func New(username string) (*ChatWorker, error) {
	if username == "" {
		return nil, errors.New("username can't be empty")
	}

	return &ChatWorker{
			username: username,
			reader:   bufio.NewReader(os.Stdin)},
		nil
}
