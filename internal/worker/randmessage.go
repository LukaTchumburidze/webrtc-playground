package worker

import (
	"errors"
	"github.com/pion/randutil"
	"github.com/sirupsen/logrus"
	"time"
	"webrtc-playground/internal/logger"
)

const (
	MsgLength            = 30
	DelayForProducingMsg = 5 * time.Second
)

var (
	ErrInvalidNumberOfMessages = errors.New("number of messages should be positive")
)

func RandSeq(n int) string {
	val, err := randutil.GenerateCryptoRandomString(n, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	if err != nil {
		panic(err)
	}

	return val
}

type RandMessageWorker struct {
	nOfMessages uint
	msgCnt      uint
}

func (receiver *RandMessageWorker) ProducePayload() ([]byte, error) {
	if receiver.msgCnt == receiver.nOfMessages {
		return nil, ErrFinish
	}
	time.Sleep(DelayForProducingMsg)

	b := []byte(RandSeq(MsgLength))
	logger.Logger.WithFields(logrus.Fields{
		"cnt":     receiver.msgCnt,
		"payload": string(b),
	}).Info("Produced Payload")

	receiver.msgCnt++
	return b, nil
}

func (receiver *RandMessageWorker) ConsumePayload(bytes []byte) error {
	logger.Logger.WithField("payload", string(bytes)).Info("Consumed Payload")

	return nil
}

func NewRandMessageWorker(nOfMessages uint) (*RandMessageWorker, error) {
	if nOfMessages <= 0 {
		return nil, ErrInvalidNumberOfMessages
	}

	return &RandMessageWorker{
		nOfMessages: nOfMessages,
	}, nil
}
