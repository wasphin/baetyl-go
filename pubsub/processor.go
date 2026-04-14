package pubsub

import (
	"time"

	"github.com/baetyl/baetyl-go/v2/errors"
	"github.com/baetyl/baetyl-go/v2/log"
	v1 "github.com/baetyl/baetyl-go/v2/spec/v1"
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
	for {
		select {
		case msg := <-p.channel:
			if p.handler != nil {
				if err := p.handler.OnMessage(msg); err != nil {
					p.log.Error("failed to handle message", log.Error(err))
				}
			}
		case <-p.tomb.Dying():
			return nil
		}
	}
}

// KeyFunc extracts a partition key from a message.
// Messages with the same key are dispatched to the same worker and processed sequentially.
// Messages with different keys may be processed concurrently by different workers.
type KeyFunc func(msg interface{}) string

const defaultWorkerNum = 16
const defaultWorkerChanSize = 100

// NewOrderedProcessor creates a processor that guarantees messages with the same
// partition key (returned by keyFunc) are processed sequentially.
// workerNum controls the number of parallel workers; use 0 for defaultWorkerNum (16).
func NewOrderedProcessor(ch <-chan interface{}, workerNum int, handler Handler) Processor {
	if workerNum <= 0 {
		workerNum = defaultWorkerNum
	}
	workers := make([]chan interface{}, workerNum)
	for i := range workers {
		workers[i] = make(chan interface{}, defaultWorkerChanSize)
	}
	return &orderedProcessor{
		channel:   ch,
		handler:   handler,
		workerNum: workerNum,
		workers:   workers,
		log:       log.L().With(log.Any("pubsub", "ordered-processor")),
	}
}

type orderedProcessor struct {
	channel   <-chan interface{}
	handler   Handler
	workerNum int
	workers   []chan interface{}
	tomb      utils.Tomb
	log       *log.Logger
}

func (p *orderedProcessor) Start() {
	for i := range p.workers {
		workerCh := p.workers[i] // 避免循环变量捕获
		p.tomb.Go(func() error {
			return p.runWorker(workerCh)
		})
	}
	p.tomb.Go(p.dispatch)
}

func (p *orderedProcessor) Close() {
	p.tomb.Kill(nil)
	p.tomb.Wait()
}

// dispatch 是单 goroutine，负责将消息按 key 路由到对应 worker channel。
// 接收链路不被阻塞，只做路由投递。
func (p *orderedProcessor) dispatch() error {
	for {
		select {
		case msg := <-p.channel:
			if p.handler == nil {
				p.log.Error("failed to handle message", log.Error(ErrProcessorInvalidHandler))
				return ErrProcessorInvalidHandler
			}
			key := getPartitionKey(msg)
			idx := fnv32a(key) % uint32(p.workerNum)
			select {
			case p.workers[idx] <- msg:
			default:
				p.log.Warn("worker channel full, message dropped",
					log.Error(ErrProcessorToManyMessages), log.Any("key", key))
			}
		case <-p.tomb.Dying():
			return nil
		}
	}
}

// runWorker 在独立 goroutine 中串行消费同一分区的消息，保证该分区内有序。
func (p *orderedProcessor) runWorker(ch <-chan interface{}) error {
	for {
		select {
		case msg := <-ch:
			if err := p.handler.OnMessage(msg); err != nil {
				p.log.Error("failed to handle message", log.Error(err))
			}
		case <-p.tomb.Dying():
			return nil
		}
	}
}

// fnv32a 是 FNV-1a 32位哈希，速度快、分布均匀，适合短字符串的分区映射。
func fnv32a(key string) uint32 {
	const (
		offset32 = uint32(2166136261)
		prime32  = uint32(16777619)
	)
	h := offset32
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= prime32
	}
	return h
}

func getPartitionKey(msg interface{}) string {
	if m, ok := msg.(*v1.Message); ok {
		return m.PartitionKey()
	}
	return ""
}
