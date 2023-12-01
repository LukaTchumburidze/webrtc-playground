package mode

const (
	WORKER_RAND_MESSAGES = "rand_messages"
	WORKER_CHAT          = "chat"
	WORKER_FS            = "fs"
)

type Worker interface {
	ProducePayload() ([]byte, error)
	ConsumePayload([]byte) error
}
