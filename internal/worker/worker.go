package worker

import "errors"

const (
	WORKER_RAND_MESSAGES = "rand_messages"
	WORKER_CHAT          = "chat"
	WORKER_FS            = "fs"
)

var ErrFinish = errors.New("worker finished")

type Worker interface {
	ProducePayload() ([]byte, error)
	ConsumePayload([]byte) error
}
