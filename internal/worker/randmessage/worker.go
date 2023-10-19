package randmessage

import (
	"errors"
	"fmt"
	"github.com/pion/randutil"
	"time"
)

const (
	DEFAULT_N_OF_MESSAGES   = 5
	MSG_LENGTH              = 30
	DELAY_FOR_PRODUCING_MSG = 5 * time.Second
)

func RandSeq(n int) string {
	val, err := randutil.GenerateCryptoRandomString(n, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	if err != nil {
		panic(err)
	}

	return val
}

type RandMessageWorker struct {
	nOfMessages int
	msgCnt      int
}

func (receiver RandMessageWorker) ProducePayload() ([]byte, error) {
	if receiver.msgCnt == receiver.nOfMessages {
		return nil, nil
	}
	time.Sleep(DELAY_FOR_PRODUCING_MSG)

	b := []byte(RandSeq(MSG_LENGTH))

	return b, nil
}

func (receiver RandMessageWorker) ConsumePayload(bytes []byte) error {
	fmt.Printf("%T %p received %v", receiver, receiver, string(bytes))

	return nil
}

func New(nOfMessages int) (*RandMessageWorker, error) {
	if nOfMessages <= 0 {
		return nil, errors.New("number of messages should be positive")
	}

	return &RandMessageWorker{
		nOfMessages: nOfMessages,
	}, nil
}
