package worker

import "errors"

const (
	WorkerRandMessages = "rand_messages"
	WorkerChat         = "chat"
	WorkerFS           = "fs"
)

var ErrFinish = errors.New("worker finished incorrectly")

type Worker interface {
	ProducePayload() ([]byte, error)
	ConsumePayload([]byte) error
}
