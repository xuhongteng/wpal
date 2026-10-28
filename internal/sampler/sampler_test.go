package sampler

import (
	"sync"
	"testing"
	"time"
)

// TestSubscriberKeepsLatestFrame 验证订阅者永远只拿到最新一帧，
// 慢客户端不会积压历史帧，也不会阻塞广播方。
func TestSubscriberKeepsLatestFrame(t *testing.T) {
	s := &Sampler{subs: map[int]*Subscriber{}}
	id, sub := s.Subscribe()
	defer s.Unsubscribe(id)

	// 连续推 100 帧，中间不取
	for i := 0; i < 100; i++ {
		s.broadcast([]byte{byte(i)})
	}

	// 只能取到最新一帧
	frame := sub.Take()
	if frame == nil {
		t.Fatal("应能取到一帧")
	}
	if len(frame) != 1 || frame[0] != 99 {
		t.Fatalf("应取到最新帧 99，实际 %v", frame)
	}

	// 取过之后应没有残留帧
	if again := sub.Take(); again != nil {
		t.Fatalf("不应再有残留帧，实际 %v", again)
	}
}

// TestBroadcastDoesNotBlock 验证无订阅者或订阅者不消费时广播不会卡住。
func TestBroadcastDoesNotBlock(t *testing.T) {
	s := &Sampler{subs: map[int]*Subscriber{}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			s.broadcast([]byte("frame"))
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("广播被阻塞，慢订阅者不应反压采样循环")
	}
}

// TestUnsubscribeClosesSubscriber 验证取消订阅会关闭 Closed 通道，
// 供写协程感知并退出。
func TestUnsubscribeClosesSubscriber(t *testing.T) {
	s := &Sampler{subs: map[int]*Subscriber{}}
	id, sub := s.Subscribe()
	s.Unsubscribe(id)

	select {
	case <-sub.Closed():
	case <-time.After(time.Second):
		t.Fatal("取消订阅后 Closed 通道应被关闭")
	}

	// 重复 Unsubscribe 不应 panic
	s.Unsubscribe(id)
}

// TestSubscribeConcurrent 用 -race 验证并发订阅/广播/取消的安全性。
func TestSubscribeConcurrent(t *testing.T) {
	s := &Sampler{subs: map[int]*Subscriber{}}
	var wg sync.WaitGroup

	// 持续广播
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				s.broadcast([]byte("frame"))
			}
		}
	}()

	// 并发订阅与取消
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, sub := s.Subscribe()
			_ = sub.Take()
			s.Unsubscribe(id)
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}
