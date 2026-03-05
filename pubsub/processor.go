package pubsub

import (
	"sync"
	"time"

	"github.com/baetyl/baetyl-go/v2/errors"
	"github.com/baetyl/baetyl-go/v2/log"
	"github.com/baetyl/baetyl-go/v2/utils"
)

var (
	ErrProcessorTimeout = errors.New("failed to send message because of timeout")
)

type Processor interface {
	Start()
	Close()
}

type processor struct {
	channel <-chan interface{}
	timeout time.Duration
	handler Handler
	tomb    utils.Tomb
	log     *log.Logger
}

func NewProcessor(ch <-chan interface{}, timeout time.Duration, handler Handler) Processor {
	return &processor{
		channel: ch,
		timeout: timeout,
		handler: handler,
		tomb:    utils.Tomb{},
		log:     log.L().With(log.Any("pubsub", "processor")),
	}
}

func (p *processor) Start() {
	if p.timeout > 0 {
		p.tomb.Go(p.timerProcessing)
	} else {
		p.tomb.Go(p.processing)
	}
}

func (p *processor) Close() {
	p.tomb.Kill(nil)
	p.tomb.Wait()
}

func (p *processor) timerProcessing() error {
	timer := time.NewTimer(p.timeout)
	defer timer.Stop()
	for {
		select {
		case msg := <-p.channel:
			if p.handler != nil {
				if err := p.handler.OnMessage(msg); err != nil {
					p.log.Error("failed to handle message", log.Error(err))
				}
			}
			timer.Reset(p.timeout)
		case <-timer.C:
			p.log.Warn("pubsub timeout")
			if p.handler != nil {
				if err := p.handler.OnTimeout(); err != nil {
					p.log.Error("failed to handle message because of timeout", log.Error(err))
				}
			}
			p.tomb.Kill(ErrProcessorTimeout)
		case <-p.tomb.Dying():
			return nil
		}
	}
}

func (p *processor) processing() error {
	// 启动 N 个 worker 并行处理
	workerCount := 100
	var wg sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case msg, ok := <-p.channel:
					if !ok {
						return
					}
					if p.handler != nil {
						p.handler.OnMessage(msg)
					}
				case <-p.tomb.Dying():
					return
				}
			}
		}()
	}
	wg.Wait()
	return nil
}
